package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nulzo/model-router-api/pkg/api"
)

// registry is a private helper struct to manage model definitions.
// It is thread-safe.
type registry struct {
	models map[string]api.ModelDefinition
	mu     sync.RWMutex
}

func newRegistry() *registry {
	return &registry{
		models: make(map[string]api.ModelDefinition),
	}
}

func (r *registry) addModel(m api.ModelDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[m.ID] = m
}

// func (r *registry) getModel(id string) (api.ModelDefinition, bool) {
// 	r.mu.RLock()
// 	defer r.mu.RUnlock()
// 	m, ok := r.models[id]
// 	return m, ok
// }

func (r *registry) ResolveRoute(modelID string) (string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if m, ok := r.models[modelID]; ok {
		upstreamID := m.UpstreamID
		if upstreamID == "" {
			upstreamID = modelID
		}
		return m.ProviderID, upstreamID, nil
	}

	return "", "", fmt.Errorf("model not found: %s", modelID)
}

// listAndFilter converts internal definitions to the public API response format
// and applies filters.
func (s *service) ListAllModels(ctx context.Context, filter api.ModelFilter) ([]api.Model, error) {
	s.registry.mu.RLock()
	defer s.registry.mu.RUnlock()

	var results []api.Model

	for _, def := range s.registry.models {
		m := api.Model{
			ID:            def.ID,
			Name:          def.Name,
			Provider:      def.ProviderID,
			Description:   def.Description,
			ContextLength: def.ContextLength,
			Pricing: api.Pricing{
				Prompt:            def.Pricing.Prompt,
				Completion:        def.Pricing.Completion,
				Request:           def.Pricing.Request,
				Image:             def.Pricing.Image,
				WebSearch:         def.Pricing.WebSearch,
				InternalReasoning: def.Pricing.InternalReasoning,
				InputCacheRead:    def.Pricing.InputCacheRead,
				InputCacheWrite:   def.Pricing.InputCacheWrite,
			},
			Architecture: api.Architecture{
				InputModalities:  def.Architecture.InputModalities,
				OutputModalities: def.Architecture.OutputModalities,
				Tokenizer:        def.Architecture.Tokenizer,
				InstructType:     def.Architecture.InstructType,
			},
			TopProvider: api.TopProvider{
				ContextLength:       def.TopProvider.ContextLength,
				MaxCompletionTokens: def.TopProvider.MaxCompletionTokens,
				IsModerated:         def.TopProvider.IsModerated,
			},
			OwnedBy: "system",
		}

		if filter.Provider != "" && !strings.EqualFold(m.Provider, filter.Provider) {
			continue
		}
		if filter.ID != "" && !strings.Contains(strings.ToLower(m.ID), strings.ToLower(filter.ID)) {
			continue
		}
		if filter.OwnedBy != "" && !strings.EqualFold(m.OwnedBy, filter.OwnedBy) {
			continue
		}
		if filter.Modality != "" {
			found := false
			for _, mod := range m.Architecture.InputModalities {
				if strings.EqualFold(mod, filter.Modality) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		results = append(results, m)
	}

	return results, nil
}
