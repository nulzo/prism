package router

import (
	"fmt"
	"strings"
	"sync"

	"github.com/nulzo/model-router-api/internal/provider"
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
