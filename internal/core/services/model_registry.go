package services

import (
	"fmt"
	"strings"
	"sync"

	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type InMemoryModelRegistry struct {
	models map[string]schema.ModelDefinition
	mu     sync.RWMutex
}

func NewInMemoryModelRegistry(models []schema.ModelDefinition) ports.ModelRegistry {
	index := make(map[string]schema.ModelDefinition)
	for _, m := range models {
		if m.Enabled {
			index[m.ID] = m
		}
	}
	return &InMemoryModelRegistry{
		models: index,
	}
}

func (r *InMemoryModelRegistry) GetModel(id string) (*schema.ModelDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if m, ok := r.models[id]; ok {
		return &m, nil
	}
	return nil, fmt.Errorf("model not found: %s", id)
}

func (r *InMemoryModelRegistry) ListModels() []schema.ModelDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]schema.ModelDefinition, 0, len(r.models))
	for _, m := range r.models {
		list = append(list, m)
	}
	return list
}

func (r *InMemoryModelRegistry) ResolveRoute(modelID string) (string, string, error) {
	m, err := r.GetModel(modelID)
	if err != nil {
		return "", "", err
	}
	return m.ProviderID, m.UpstreamID, nil
}

// Reload updates the registry with new definitions (thread-safe)
func (r *InMemoryModelRegistry) Reload(models []schema.ModelDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newIndex := make(map[string]schema.ModelDefinition)
	for _, m := range models {
		if m.Enabled {
			newIndex[m.ID] = m
		}
	}
	r.models = newIndex
}

// AddModel adds or updates a single model (thread-safe)
func (r *InMemoryModelRegistry) AddModel(model schema.ModelDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[model.ID] = model
}

func (r *InMemoryModelRegistry) SearchModels(query string) []schema.ModelDefinition {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    var results []schema.ModelDefinition
    query = strings.ToLower(query)
    
    for _, m := range r.models {
        if strings.Contains(strings.ToLower(m.ID), query) || strings.Contains(strings.ToLower(m.Name), query) {
            results = append(results, m)
        }
    }
    return results
}
