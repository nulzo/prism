package services

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/logger"
	"github.com/nulzo/model-router-api/pkg/schema"
	"go.uber.org/zap"
)

type RouterService struct {
	providers map[string]ports.ModelProvider
	registry  ports.ModelRegistry
	cache     ports.CacheService
	mu        sync.RWMutex
}

func NewRouterService(registry ports.ModelRegistry, cache ports.CacheService) *RouterService {
	return &RouterService{
		providers: make(map[string]ports.ModelProvider),
		registry:  registry,
		cache:     cache,
	}
}

func (s *RouterService) RegisterProvider(p ports.ModelProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.Name()] = p
}

// GetProviderForModel finds the best provider for a given model ID and returns the provider and the upstream model ID
func (s *RouterService) GetProviderForModel(ctx context.Context, modelID string) (ports.ModelProvider, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providerID, upstreamModelID, err := s.registry.ResolveRoute(modelID)
	if err != nil {
		return nil, "", domain.BadRequestError(fmt.Sprintf("route resolution failed for model '%s': %v", modelID, err))
	}

	if p, exists := s.providers[providerID]; exists {
		return p, upstreamModelID, nil
	}

	return nil, "", domain.ProviderError(fmt.Sprintf("provider '%s' configured but not active/loaded", providerID), nil)
}

func (s *RouterService) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	p, upstreamID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}

	// Create a shallow copy of the request to avoid mutating the original
	// If the request contains slices that are modified deep down, we might need a deep copy,
	// but strictly for changing the Model string, this is sufficient.
	reqClone := *req
	reqClone.Model = upstreamID

	resp, err := p.Chat(ctx, &reqClone)
	if err != nil {
		return nil, domain.ProviderError("Upstream provider request failed", err)
	}
	return resp, nil
}

func (s *RouterService) StreamChat(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	p, upstreamID, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed for stream", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}

	reqClone := *req
	reqClone.Model = upstreamID

	return p.Stream(ctx, &reqClone)
}

func (s *RouterService) ListAllModels(ctx context.Context, filter ports.ModelFilter) ([]schema.Model, error) {
	// Source of truth is now the Registry
	definitions := s.registry.ListModels()

	var models []schema.Model
	for _, def := range definitions {
		m := schema.Model{
			ID:            def.ID,
			Name:          def.Name,
			Provider:      def.ProviderID,
			Description:   def.Description,
			ContextLength: def.Config.ContextWindow,
			Pricing: schema.Pricing{
				Prompt:     fmt.Sprintf("%f", def.Pricing.Input),
				Completion: fmt.Sprintf("%f", def.Pricing.Output),
				Image:      fmt.Sprintf("%f", def.Pricing.Image),
			},
			Architecture: schema.Architecture{
				Modality: strings.Join(def.Config.Modality, ","),
			},
		}

		// Apply basic filtering
		if filter.Provider != "" && !strings.EqualFold(def.ProviderID, filter.Provider) {
			continue
		}

		models = append(models, m)
	}

	return applyFilters(models, filter), nil
}
