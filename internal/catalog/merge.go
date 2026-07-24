package catalog

import "github.com/nulzo/model-router-api/pkg/api"

// Merge combines discovered models with static YAML overrides. Static entries
// win on ID collision so operators can enrich or disable individual models
// without forking provider discovery logic.
func Merge(static, discovered []api.ModelDefinition) []api.ModelDefinition {
	byID := make(map[string]api.ModelDefinition, len(static)+len(discovered))
	for _, m := range discovered {
		byID[m.ID] = m
	}
	for _, m := range static {
		byID[m.ID] = m
	}
	out := make([]api.ModelDefinition, 0, len(byID))
	for _, m := range byID {
		if !m.Enabled {
			continue
		}
		out = append(out, m)
	}
	return out
}
