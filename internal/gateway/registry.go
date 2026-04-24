package gateway

import (
	"context"

	"github.com/nulzo/model-router-api/pkg/api"
)

// ListAllModels emits the public OpenRouter-shaped Model records.
func (s *service) ListAllModels(ctx context.Context, filter api.ModelFilter) ([]api.Model, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]api.Model, 0, len(s.models))
	for _, m := range s.models {
		// Apply filter
		if filter.Provider != "" && m.ProviderID != filter.Provider {
			continue
		}

		out = append(out, entryToPublic(m))
	}
	return out, nil
}

// entryToPublic projects an api.ModelDefinition into the OpenRouter-aligned public Model shape.
func entryToPublic(e api.ModelDefinition) api.Model {
	owned := e.ProviderID
	if owned == "" {
		owned = "system"
	}
	canonical := e.CanonicalSlug
	if canonical == "" {
		canonical = e.ID
	}
	m := api.Model{
		ID:                  e.ID,
		CanonicalSlug:       canonical,
		HuggingFaceID:       e.HuggingFaceID,
		Object:              "model",
		OwnedBy:             owned,
		Provider:            e.ProviderID,
		Name:                e.Name,
		Description:         e.Description,
		ContextLength:       e.ContextLength,
		SupportedParameters: append([]string(nil), e.SupportedParameters...),
		DefaultParameters:   cloneMap(e.DefaultParameters),
		Architecture: api.Architecture{
			InputModalities:  e.Architecture.InputModalities,
			OutputModalities: e.Architecture.OutputModalities,
			Tokenizer:        e.Architecture.Tokenizer,
			InstructType:     e.Architecture.InstructType,
		},
		Pricing: api.Pricing{
			Prompt:            e.Pricing.Prompt,
			Completion:        e.Pricing.Completion,
			Request:           e.Pricing.Request,
			Image:             e.Pricing.Image,
			WebSearch:         e.Pricing.WebSearch,
			InternalReasoning: e.Pricing.InternalReasoning,
			InputCacheRead:    e.Pricing.InputCacheRead,
			InputCacheWrite:   e.Pricing.InputCacheWrite,
		},
		TopProvider: api.TopProvider{
			ContextLength:       e.TopProvider.ContextLength,
			MaxCompletionTokens: e.TopProvider.MaxCompletionTokens,
			IsModerated:         e.TopProvider.IsModerated,
		},
		PerRequestLimits: e.PerRequestLimits,
		KnowledgeCutoff:  e.KnowledgeCutoff,
		ExpirationDate:   e.ExpirationDate,
		Links:            e.Links,
	}
	if !e.LastUpdated.IsZero() {
		m.Created = e.LastUpdated.Unix()
	}
	return m
}

// cloneMap returns a defensive copy so the public Model response can't be
// mutated through a shared reference into the catalog snapshot.
func cloneMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
