package services

import (
	"strings"

	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
)

func applyFilters(models []schema.Model, filter ports.ModelFilter) []schema.Model {
	filtered := make([]schema.Model, 0)
	for _, m := range models {
		if matchesFilter(m, filter) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func matchesFilter(m schema.Model, f ports.ModelFilter) bool {
	if f.Provider != "" && !strings.EqualFold(m.Provider, f.Provider) {
		return false
	}
	if f.ID != "" && !strings.Contains(strings.ToLower(m.ID), strings.ToLower(f.ID)) {
		return false
	}
	if f.OwnedBy != "" && !strings.EqualFold(m.OwnedBy, f.OwnedBy) {
		return false
	}
	if f.Modality != "" && !strings.EqualFold(m.Architecture.Modality, f.Modality) {
		return false
	}
	return true
}

func matchProviderHeuristic(modelID string, p ports.ModelProvider) bool {
	lowered := strings.ToLower(modelID)
	if strings.Contains(lowered, "gpt") && p.Type() == "openai" {
		return true
	}
	if strings.Contains(lowered, "claude") && p.Type() == "anthropic" {
		return true
	}
	if strings.Contains(lowered, "gemini") && p.Type() == "google" {
		return true
	}
	return false
}
