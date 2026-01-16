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
