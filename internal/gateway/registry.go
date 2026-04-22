package gateway

import (
	"context"

	"github.com/nulzo/model-router-api/internal/catalog"
	"github.com/nulzo/model-router-api/pkg/api"
)

// ListAllModels is a thin adapter over Catalog.Filter that emits the
// public OpenRouter-shaped Model records. The gateway is intentionally
// dumb here: all non-trivial work lives in the catalog so the HTTP
// handler and every other consumer sees the same truth.
func (s *service) ListAllModels(ctx context.Context, filter api.ModelFilter) ([]api.Model, error) {
	entries := s.catalog.Filter(filter)
	out := make([]api.Model, 0, len(entries))
	for _, e := range entries {
		out = append(out, entryToPublic(e))
	}
	return out, nil
}

// entryToPublic projects a catalog.Entry into the OpenRouter-aligned public
// Model shape. OwnedBy defaults to the provider id (which is what
// OpenRouter exposes for its own listings) but falls back to "system" for
// entries with no provider attached.
func entryToPublic(e catalog.Entry) api.Model {
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
