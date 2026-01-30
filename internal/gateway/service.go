package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nulzo/model-router-api/internal/analytics"
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
	ingestor  analytics.Ingestor
	cache     cache.CacheService
	mu        sync.RWMutex
	providers map[string]llm.Provider
	registry  *registry
}

func NewService(logger *zap.Logger, repo store.Repository, ingestor analytics.Ingestor, cache cache.CacheService) Service {
	return &service{
		logger:    logger,
		repo:      repo,
		ingestor:  ingestor,
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

	var userID, apiKeyID, appName string

	if val, ok := ctx.Value(store.ContextKeyAppName).(string); ok {
		appName = val
	}

	if apiKey, ok := ctx.Value(store.ContextKeyAPIKey).(*model.APIKey); ok {
		userID = apiKey.UserID
		apiKeyID = apiKey.ID
	} else {
		if appName != "" {
			userID = string(api.Anonymous)
			apiKeyID = string(api.Anonymous)
		} else {
			userID = string(api.System)
			apiKeyID = string(api.System)
		}
	}

	finishReason := ""
	if len(resp.Choices) > 0 {
		finishReason = resp.Choices[0].FinishReason
	}

	log := &model.RequestLog{
		ID:               resp.ID,
		UserID:           userID,
		APIKeyID:         apiKeyID,
		AppName:          appName,
		ProviderID:       provider.Name(),
		ModelID:          req.Model,
		UpstreamModelID:  upstreamModelID,
		UpstreamRemoteID: resp.ID,
		FinishReason:     finishReason,
		StatusCode:       200,
		LatencyMS:        latency.Milliseconds(),
		CreatedAt:        time.Now(),
	}

	if resp.Usage != nil {
		log.InputTokens = resp.Usage.PromptTokens
		log.OutputTokens = resp.Usage.CompletionTokens
	}

	pricing, err := s.repo.Providers().GetModelPricing(context.Background(), req.Model)
	if err == nil && pricing != nil && resp.Usage != nil {
		inputCost := (int64(resp.Usage.PromptTokens) * pricing.InputCostMicrosPer1k) / 1000
		outputCost := (int64(resp.Usage.CompletionTokens) * pricing.OutputCostMicrosPer1k) / 1000
		log.TotalCostMicros = inputCost + outputCost
	}

	s.ingestor.Log(log)

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
	provider, upstreamID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed for stream", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}

	reqClone := *req
	reqClone.Model = upstreamID

	streamChan, err := provider.Stream(ctx, &reqClone)
	if err != nil {
		return nil, err
	}

	// Intercept stream for logging
	outChan := make(chan api.StreamResult)

	go func() {
		defer close(outChan)

		start := time.Now()
		var ttft *time.Duration
		var inputTokens, outputTokens int
		var finishReason string
		var lastID string

		// Capture identity context before loop (context might be cancelled but values persist)
		var userID, apiKeyID, appName string
		if val, ok := ctx.Value(store.ContextKeyAppName).(string); ok {
			appName = val
		}
		if apiKey, ok := ctx.Value(store.ContextKeyAPIKey).(*model.APIKey); ok {
			userID = apiKey.UserID
			apiKeyID = apiKey.ID
		} else {
			if appName != "" {
				userID = string(api.Anonymous)
				apiKeyID = string(api.Anonymous)
			} else {
				userID = string(api.System)
				apiKeyID = string(api.System)
			}
		}

		for result := range streamChan {
			// Record TTFT on first successful token
			if ttft == nil && result.Response != nil {
				dur := time.Since(start)
				ttft = &dur
			}

			if result.Response != nil {
				lastID = result.Response.ID

				// Capture usage if provided (some providers send it in last chunk)
				if result.Response.Usage != nil {
					inputTokens = result.Response.Usage.PromptTokens
					outputTokens = result.Response.Usage.CompletionTokens
				}

				// If choices present
				if len(result.Response.Choices) > 0 {
					if result.Response.Choices[0].FinishReason != "" {
						finishReason = result.Response.Choices[0].FinishReason
					}
				}
			}

			outChan <- result
		}

		// Log after stream closes
		latency := time.Since(start)
		var ttftMS sql.NullInt64
		if ttft != nil {
			ttftMS = sql.NullInt64{Int64: ttft.Milliseconds(), Valid: true}
		}

		log := &model.RequestLog{
			ID:               lastID, // Might be empty if stream failed immediately
			UserID:           userID,
			APIKeyID:         apiKeyID,
			AppName:          appName,
			ProviderID:       provider.Name(),
			ModelID:          req.Model,
			UpstreamModelID:  upstreamID,
			UpstreamRemoteID: lastID,
			FinishReason:     finishReason,
			StatusCode:       200,
			LatencyMS:        latency.Milliseconds(),
			TTFTMS:           ttftMS,
			CreatedAt:        time.Now(),
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
		}

		if log.ID == "" {
			log.ID = fmt.Sprintf("stream-fail-%d", time.Now().UnixNano())
			log.StatusCode = 500
		}

		// Calculate cost
		pricing, err := s.repo.Providers().GetModelPricing(context.Background(), req.Model)
		if err == nil && pricing != nil {
			inputCost := (int64(inputTokens) * pricing.InputCostMicrosPer1k) / 1000
			outputCost := (int64(outputTokens) * pricing.OutputCostMicrosPer1k) / 1000
			log.TotalCostMicros = inputCost + outputCost
		}

		s.ingestor.Log(log)
	}()

	return outChan, nil
}
