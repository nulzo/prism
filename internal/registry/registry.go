package registry

import (
	"fmt"
	"sync"

	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
)

// Factory is a function that creates a ModelProvider instance given a configuration.
// We use the domain.ProviderConfig struct which serves as our unified configuration shape.
type Factory func(cfg domain.ProviderConfig) (ports.ModelProvider, error)

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory)
)

// Register makes a provider factory available to the system.
// 'type' is the key (e.g., "openai", "ollama").
func Register(providerType string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := factories[providerType]; exists {
		panic(fmt.Sprintf("provider factory %s already registered", providerType))
	}
	factories[providerType] = f
}

// Get retrieves a factory to create a provider of a specific type.
func Get(providerType string) (Factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[providerType]
	if !ok {
		return nil, fmt.Errorf("provider factory not found for type: %s", providerType)
	}
	return f, nil
}
