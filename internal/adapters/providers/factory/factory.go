package factory

import (
	"fmt"

	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/registry"
)

type ProviderFactory struct{}

func NewProviderFactory() *ProviderFactory {
	return &ProviderFactory{}
}

func (f *ProviderFactory) CreateProvider(cfg domain.ProviderConfig) (ports.ModelProvider, error) {
	// Look up the factory function in the registry
	factoryFunc, err := registry.Get(cfg.Type)
	if err != nil {
		return nil, fmt.Errorf("factory lookup failed for type %s: %w", cfg.Type, err)
	}
	
	// Create the provider instance
	return factoryFunc(cfg)
}
