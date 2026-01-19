package llm

import (
	"fmt"
	"sync"

	"github.com/nulzo/model-router-api/internal/config"
)

type ProviderFactory struct{}

func NewProviderFactory() *ProviderFactory {
	return &ProviderFactory{}
}

func (f *ProviderFactory) CreateProvider(cfg config.ProviderConfig) (Provider, error) {
	// Look up the factory function in the registry
	factoryFunc, err := Get(cfg.Type)
	if err != nil {
		return nil, fmt.Errorf("factory lookup failed for type %s: %w", cfg.Type, err)
	}

	// Create the provider instance
	return factoryFunc(cfg)
}

type Factory func(cfg config.ProviderConfig) (Provider, error)

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory)
)

func Register(providerType string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[providerType]; exists {
		panic(fmt.Sprintf("provider factory %s already registered", providerType))
	}
	factories[providerType] = f
}

func Get(providerType string) (Factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[providerType]
	if !ok {
		return nil, fmt.Errorf("provider factory not found for type: %s", providerType)
	}
	return f, nil
}
