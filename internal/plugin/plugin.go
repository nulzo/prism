package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/nulzo/model-router-api/pkg/api"
)

// Context holds the state for a single request pipeline execution.
type Context struct {
	Request  *api.ChatRequest
	Response *api.ChatResponse // Nil during PreProcess
	// Add logger, metrics, etc. here
}

// Plugin defines the interface for deterministic request/response middleware.
type Plugin interface {
	ID() string
	PreProcess(ctx context.Context, pCtx *Context) error
	PostProcess(ctx context.Context, pCtx *Context) error
}

// Registry manages available plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

func (r *Registry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.ID()] = p
}

func (r *Registry) Get(id string) (Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	if !ok {
		return nil, fmt.Errorf("plugin not found: %s", id)
	}
	return p, nil
}
