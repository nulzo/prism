package catalog_test

import (
	"testing"

	"github.com/nulzo/model-router-api/internal/catalog"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestMerge_StaticOverridesDiscovered(t *testing.T) {
	discovered := []api.ModelDefinition{
		{ID: "ollama/llama3:latest", ProviderID: "ollama", UpstreamID: "llama3:latest", Enabled: true, ContextLength: 8192},
		{ID: "ollama/phi3:latest", ProviderID: "ollama", UpstreamID: "phi3:latest", Enabled: true},
	}
	static := []api.ModelDefinition{
		{ID: "ollama/llama3:latest", ProviderID: "ollama", UpstreamID: "llama3:latest", Enabled: true, ContextLength: 32768},
	}

	merged := catalog.Merge(static, discovered)

	assert.Len(t, merged, 2)
	byID := indexByID(merged)
	assert.Equal(t, 32768, byID["ollama/llama3:latest"].ContextLength)
}

func TestMerge_SkipsDisabledModels(t *testing.T) {
	merged := catalog.Merge(
		[]api.ModelDefinition{{ID: "ollama/hidden:latest", Enabled: false}},
		[]api.ModelDefinition{{ID: "ollama/visible:latest", Enabled: true}},
	)
	assert.Len(t, merged, 1)
	assert.Equal(t, "ollama/visible:latest", merged[0].ID)
}

func indexByID(models []api.ModelDefinition) map[string]api.ModelDefinition {
	out := make(map[string]api.ModelDefinition, len(models))
	for _, m := range models {
		out[m.ID] = m
	}
	return out
}
