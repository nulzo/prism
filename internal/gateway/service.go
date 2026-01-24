package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

var (
	ErrProviderNotFound = errors.New("provider not found")
	ErrRouteNotFound    = errors.New("no provider configured for this model")
)

// Service defines the business logic for routing requests.
type Service interface {
	// RegisterProvider registers a new model provider and syncs its models
	RegisterProvider(ctx context.Context, p llm.Provider) error

	GetProviderForModel(ctx context.Context, modelID string) (llm.Provider, string, error)
	ListAllModels(ctx context.Context, filter api.ModelFilter) ([]api.Model, error)
	Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error)
	StreamChat(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error)
}

type service struct {
	logger    *zap.Logger
	repo      store.Repository
	cache     cache.CacheService
	mu        sync.RWMutex
	providers map[string]llm.Provider
	registry  *registry
}

func NewService(logger *zap.Logger, repo store.Repository, cache cache.CacheService) Service {
	return &service{
		logger:    logger,
		repo:      repo,
		cache:     cache,
		providers: make(map[string]llm.Provider),
		registry:  newRegistry(),
	}
}

func (s *service) RegisterProvider(ctx context.Context, p llm.Provider) error {
	models, _ := p.Models(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.providers[p.Name()] = p

	for _, m := range models {
		s.registry.addModel(m)
	}

	return nil
}

func (s *service) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	provider, upstreamModelID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}

	reqClone := *req
	reqClone.Model = upstreamModelID

	start := time.Now()
	resp, err := provider.Chat(ctx, &reqClone)
	if err != nil {
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}

	latency := time.Since(start)

	// Persist usage to DB (Async to avoid blocking response)
	go func() {
		apiKey, ok := ctx.Value(store.ContextKeyAPIKey).(*model.APIKey)
		if !ok {
			s.logger.Warn("API Key not found in context, skipping usage logging")
			return
		}

		log := &model.RequestLog{
			ID:              resp.ID,
			UserID:          apiKey.UserID,
			APIKeyID:        apiKey.ID,
			ProviderID:      provider.Name(),
			ModelID:         req.Model,
			StatusCode:      200,
			LatencyMS:       latency.Milliseconds(),
			CreatedAt:       time.Now(),
		}

		if resp.Usage != nil {
			log.InputTokens = resp.Usage.PromptTokens
			log.OutputTokens = resp.Usage.CompletionTokens
		}

		// Calculate cost if pricing is available
		pricing, err := s.repo.Providers().GetModelPricing(context.Background(), req.Model)
		if err == nil && pricing != nil && resp.Usage != nil {
			inputCost := (int64(resp.Usage.PromptTokens) * pricing.InputCostMicrosPer1k) / 1000
			outputCost := (int64(resp.Usage.CompletionTokens) * pricing.OutputCostMicrosPer1k) / 1000
			log.TotalCostMicros = inputCost + outputCost
		}

		if err := s.repo.Requests().Log(context.Background(), log); err != nil {
			s.logger.Error("failed to log request", zap.Error(err))
		}
	}()

	return resp, nil
}

// GetProviderForModel finds the best provider for a given model ID and returns the provider and the upstream model ID
func (s *service) GetProviderForModel(ctx context.Context, modelID string) (llm.Provider, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providerID, upstreamModelID, err := s.registry.ResolveRoute(modelID)
	if err != nil {
		return nil, "", api.BadRequestError(fmt.Sprintf("route resolution failed for model '%s': %v", modelID, err))
	}

	if p, exists := s.providers[providerID]; exists {
		return p, upstreamModelID, nil
	}

	return nil, "", api.ProviderError(fmt.Sprintf("provider '%s' configured but not active/loaded", providerID), nil)
}

func (s *service) GetProvider(providerID string) (llm.Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.providers[providerID]; exists {
		return p, nil
	}

	return nil, api.ProviderError(fmt.Sprintf("provider '%s' configured but not active/loaded", providerID), nil)
}

func (s *service) StreamChat(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	p, upstreamID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed for stream", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}

	reqClone := *req
	reqClone.Model = upstreamID

	return p.Stream(ctx, &reqClone)
}
