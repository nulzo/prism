package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/logger"
	"github.com/nulzo/model-router-api/pkg/schema"
	"go.uber.org/zap"
)

type RouterService struct {
	providers  map[string]ports.ModelProvider
	modelIndex map[string]string
	routes     []domain.RouteConfig
	cache      ports.CacheService
	mu         sync.RWMutex
}

func NewRouterService(cache ports.CacheService) *RouterService {
	return &RouterService{
		providers:  make(map[string]ports.ModelProvider),
		modelIndex: make(map[string]string),
		routes:     make([]domain.RouteConfig, 0),
		cache:      cache,
	}
}

func (s *RouterService) RegisterProvider(p ports.ModelProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.Name()] = p
}

// RegisterModels associates a list of models with a provider for direct lookup
func (s *RouterService) RegisterModels(providerID string, models []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range models {
		s.modelIndex[m] = providerID
	}
}

func (s *RouterService) SetRoutes(routes []domain.RouteConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = routes
}

// GetProviderForModel finds the best provider for a given model ID
func (s *RouterService) GetProviderForModel(ctx context.Context, modelID string) (ports.ModelProvider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Check explicit routes (manual overrides / pattern matching)
	for _, route := range s.routes {
		if strings.HasPrefix(modelID, route.Pattern) {
			if p, exists := s.providers[route.TargetID]; exists {
				return p, nil
			}
		}
	}

	// 2. Check exact model match in index
	if providerID, ok := s.modelIndex[modelID]; ok {
		if p, exists := s.providers[providerID]; exists {
			return p, nil
		}
	}

	// 3. Fallback: Heuristic matching for well-known prefixes if not found
	// This is a safety net.
	for _, p := range s.providers {
		if matchProviderHeuristic(modelID, p) {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", modelID)
}

func (s *RouterService) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	p, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}
	return p.Chat(ctx, req)
}

func (s *RouterService) StreamChat(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	p, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		logger.Warn("Provider routing failed for stream", zap.String("model", req.Model), zap.Error(err))
		return nil, err
	}
	return p.Stream(ctx, req)
}

const ModelsCacheKey = "all_models"
const ModelsCacheTTL = 5 * time.Minute

func (s *RouterService) ListAllModels(ctx context.Context, filter ports.ModelFilter) ([]schema.Model, error) {
	// 1. Try Cache
	var allModels []schema.Model
	cacheHit := false
	if s.cache != nil {
		if err := s.cache.Get(ctx, ModelsCacheKey, &allModels); err == nil {
			cacheHit = true
		}
	}

	// 2. Fetch from Providers if not in cache
	if !cacheHit {
		s.mu.RLock()
		// Filter providers if a specific provider is requested
		var providers []ports.ModelProvider
		if filter.Provider != "" {
			for _, p := range s.providers {
				// Match by ID (Name) or Type
				if strings.EqualFold(p.Name(), filter.Provider) || strings.EqualFold(p.Type(), filter.Provider) {
					providers = append(providers, p)
				}
			}
		} else {
			// Otherwise use all providers
			providers = make([]ports.ModelProvider, 0, len(s.providers))
			for _, p := range s.providers {
				providers = append(providers, p)
			}
		}
		s.mu.RUnlock()

		var wg sync.WaitGroup
		modelsChan := make(chan []schema.Model, len(providers))

		for _, p := range providers {
			wg.Add(1)
			go func(p ports.ModelProvider) {
				defer wg.Done()
				// log but don't fail everything
				m, err := p.Models(ctx)
				if err != nil {
					logger.Warn("Failed to list models for provider", zap.String("provider", p.Name()), zap.Error(err))
					return
				}
				modelsChan <- m
			}(p)
		}

		wg.Wait()
		close(modelsChan)

		seen := make(map[string]bool)
		for ms := range modelsChan {
			for _, m := range ms {
				if !seen[m.ID] {
					allModels = append(allModels, m)
					seen[m.ID] = true
				}
			}
		}

		// Cache the full list ONLY if we queried ALL providers (no filter)
		// Otherwise, we are caching a partial list which is bad.
		if s.cache != nil && len(allModels) > 0 && filter.Provider == "" {
			_ = s.cache.Set(ctx, ModelsCacheKey, allModels, ModelsCacheTTL)
		}
	}

	effectiveFilter := filter
	if filter.Provider != "" {
		effectiveFilter.Provider = ""
	}

	return applyFilters(allModels, effectiveFilter), nil
}
