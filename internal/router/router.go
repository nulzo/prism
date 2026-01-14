package router

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Router struct {
	providers map[string]provider.ModelProvider
	routes    map[string]string // pattern -> providerName
	mu        sync.RWMutex
}

func NewRouter() *Router {
	return &Router{
		providers: make(map[string]provider.ModelProvider),
		routes:    make(map[string]string),
	}
}

func (r *Router) RegisterProvider(p provider.ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Router) RegisterRoute(pattern, providerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[pattern] = providerName
}

func (r *Router) GetProvider(model string) (provider.ModelProvider, error) {
	r.mu.RLock()
	providerName := r.matchProvider(model)
	p, exists := r.providers[providerName]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no provider configuration found for model: %s", model)
	}

	return p, nil
}

func (r *Router) matchProvider(model string) string {
	// 1. Exact match overrides
	if p, ok := r.routes[model]; ok {
		return p
	}

	// 2. Prefix matching
	for pattern, p := range r.routes {
		if strings.HasPrefix(model, pattern) {
			return p
		}
	}

	// Default fallback (could be configurable)
	return "openai"
}

// ListModels aggregates models from all registered providers concurrently
func (r *Router) ListModels(ctx context.Context) ([]schema.Model, error) {
	r.mu.RLock()
	// Snapshot providers to avoid holding lock during network calls
	providers := make([]provider.ModelProvider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	r.mu.RUnlock()

	var wg sync.WaitGroup
	modelsChan := make(chan []schema.Model, len(providers))

	for _, p := range providers {
		wg.Add(1)
		go func(p provider.ModelProvider) {
			defer wg.Done()
			// We suppress errors here to return partial results if one provider is down
			// In a production system, we might want to log this.
			if m, err := p.Models(ctx); err == nil {
				modelsChan <- m
			}
		}(p)
	}

	wg.Wait()
	close(modelsChan)

	var allModels []schema.Model
	for ms := range modelsChan {
		allModels = append(allModels, ms...)
	}

	return allModels, nil
}