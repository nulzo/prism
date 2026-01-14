package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type RouterService struct {
	providers map[string]ports.ModelProvider
	routes    []domain.RouteConfig
	cache     ports.CacheService
	mu        sync.RWMutex
}

func NewRouterService(cache ports.CacheService) *RouterService {
	return &RouterService{
		providers: make(map[string]ports.ModelProvider),
		routes:    make([]domain.RouteConfig, 0),
		cache:     cache,
	}
}

func (s *RouterService) RegisterProvider(p ports.ModelProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[p.Name()] = p
}

func (s *RouterService) SetRoutes(routes []domain.RouteConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = routes
}

// GetProviderForModel finds the best provider for a given model ID
func (s *RouterService) GetProviderForModel(ctx context.Context, modelID string) (ports.ModelProvider, error) {
	s.mu.RLock()
	// 1. Check explicit routes (manual overrides)
	for _, route := range s.routes {
		if strings.HasPrefix(modelID, route.Pattern) {
			targetID := route.TargetID
			s.mu.RUnlock()
			if p, exists := s.providers[targetID]; exists {
				return p, nil
			}
			s.mu.RLock()
		}
	}
	s.mu.RUnlock()

	// 2. Dynamic Lookup: Search all provider models
	allModels, err := s.ListAllModels(ctx)
	if err == nil {
		for _, m := range allModels {
			// Exact match or match without version tag (e.g., "tinydolphin" matches "tinydolphin:latest")
			if m.ID == modelID || strings.Split(m.ID, ":")[0] == modelID {
				s.mu.RLock()
				p, exists := s.providers[m.Provider]
				s.mu.RUnlock()
				if exists {
					return p, nil
				}
			}
		}
	}

	// 3. Fallback: Heuristic matching for well-known prefixes
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.providers {
		lowered := strings.ToLower(modelID)
		if strings.Contains(lowered, "gpt") && p.Type() == "openai" {
			return p, nil
		}
		if strings.Contains(lowered, "claude") && p.Type() == "anthropic" {
			return p, nil
		}
		if strings.Contains(lowered, "gemini") && p.Type() == "google" {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", modelID)
}

func (s *RouterService) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	p, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}
	return p.Chat(ctx, req)
}

func (s *RouterService) StreamChat(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	p, err := s.GetProviderForModel(ctx, req.Model)
	if err != nil {
		return nil, err
	}
	return p.Stream(ctx, req)
}

const ModelsCacheKey = "all_models"
const ModelsCacheTTL = 5 * time.Minute

func (s *RouterService) ListAllModels(ctx context.Context) ([]schema.Model, error) {
	// 1. Try Cache
	if s.cache != nil {
		var cachedModels []schema.Model
		if err := s.cache.Get(ctx, ModelsCacheKey, &cachedModels); err == nil {
			return cachedModels, nil
		}
	}

	// 2. Fetch from Providers
	s.mu.RLock()
	providers := make([]ports.ModelProvider, 0, len(s.providers))
	for _, p := range s.providers {
		providers = append(providers, p)
	}
	s.mu.RUnlock()

	var wg sync.WaitGroup
	modelsChan := make(chan []schema.Model, len(providers))

	for _, p := range providers {
		wg.Add(1)
		go func(p ports.ModelProvider) {
			defer wg.Done()
			if m, err := p.Models(ctx); err == nil {
				modelsChan <- m
			}
		}(p)
	}

	wg.Wait()
	close(modelsChan)

	var allModels []schema.Model
	seen := make(map[string]bool)

	for ms := range modelsChan {
		for _, m := range ms {
			if !seen[m.ID] {
				allModels = append(allModels, m)
				seen[m.ID] = true
			}
		}
	}

	// 3. Set Cache
	if s.cache != nil && len(allModels) > 0 {
		// Log error in real app
		_ = s.cache.Set(ctx, ModelsCacheKey, allModels, ModelsCacheTTL)
	}

	return allModels, nil
}
