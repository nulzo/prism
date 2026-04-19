package extension

import (
	"context"
	"fmt"
	"sync"

	"github.com/nulzo/model-router-api/pkg/api"
)

// Extension defines a tool that is executed by the router, not the client.
// It is router-native and provider-agnostic: extensions expose a callable tool
// definition to the model and execute server-side when invoked by the router.
type Extension interface {
	// Name must match the function name the model will call (e.g., "prism:datetime")
	Name() string
	// BuildTool returns the tool definition for a specific extension config.
	BuildTool(config api.ExtensionConfig) (api.Tool, error)
	// Execute runs the extension logic with the provided JSON arguments and
	// request-level extension configuration.
	Execute(ctx context.Context, config api.ExtensionConfig, args []byte) (string, error)
}

// Registry manages available extensions.
type Registry struct {
	mu         sync.RWMutex
	extensions map[string]Extension
}

func NewRegistry() *Registry {
	return &Registry{
		extensions: make(map[string]Extension),
	}
}

func (r *Registry) Register(e Extension) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extensions[e.Name()] = e
}

func (r *Registry) Get(name string) (Extension, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.extensions[name]
	if !ok {
		return nil, fmt.Errorf("extension not found: %s", name)
	}
	return e, nil
}

func (r *Registry) GetAll() []Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var exts []Extension
	for _, e := range r.extensions {
		exts = append(exts, e)
	}
	return exts
}
