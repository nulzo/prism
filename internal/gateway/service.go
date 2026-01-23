package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/store"
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
	cache     store.Store
	mu        sync.RWMutex
	providers map[string]llm.Provider
	registry  *registry
}

func NewService(logger *zap.Logger, cache store.Store) Service {
	return &service{
		logger:    logger,
		cache:     cache,
		providers: make(map[string]llm.Provider),
		registry:  newRegistry(),
	}
}

func (s *service) RegisterProvider(ctx context.Context, p llm.Provider) error {
	models, err := p.Models(ctx)

	if err != nil {
		msg := fmt.Sprintf("%s %s %s",
			cli.CrossMark(),                                      // Red X
			cli.Style(p.Name(), cli.Cyan),                        // Provider Name
			cli.Style(fmt.Sprintf("(Failed: %v)", err), cli.Red), // Error in Red
		)
		s.logger.Error(msg)
		return fmt.Errorf("failed to sync models for %s: %w", p.Name(), err)
	}

	if len(models) == 0 {
		msg := fmt.Sprintf("%s %s %s",
			cli.CrossMark(),
			cli.Style(p.Name(), cli.Cyan),
			cli.Style("0 models found", cli.Red),
		)
		s.logger.Warn(msg)
		// We return nil here because the provider is valid, just empty.
		// If you want to prevent registration entirely, return an error here.
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.providers[p.Name()] = p

	for _, m := range models {
		s.registry.addModel(m)
	}

	msg := fmt.Sprintf("%s %s %s %s",
		cli.CheckMark(),
		cli.Style(fmt.Sprintf("%s\t", p.Name()), cli.Green),
		"registered with: ",
		cli.Style(fmt.Sprintf("%d models", len(models)), cli.White),
	)

	s.logger.Info(msg)
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

	go func() {
		s.logger.Info("chat completion",
			zap.String("provider", provider.Name()),
			zap.Duration("latency", time.Since(start)),
		)
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
