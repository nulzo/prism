package registry

import (
	"fmt"
	"sync"

	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
)

type Factory func(cfg domain.ProviderConfig) (ports.ModelProvider, error)

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
