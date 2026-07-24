package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter_Models_DiscoversFromTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{
					"name":  "llama3.2:3b",
					"model": "llama3.2:3b",
					"details": map[string]interface{}{
						"family": "llama",
					},
				},
				{
					"name":  "nomic-embed-text:latest",
					"model": "nomic-embed-text:latest",
					"details": map[string]interface{}{
						"family": "bert",
					},
				},
			},
		})
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.ProviderConfig{
		ID:      "ollama",
		Type:    "ollama",
		BaseURL: server.URL + "/v1",
	})
	require.NoError(t, err)

	catalogProvider, ok := adapter.(llm.ModelCatalogProvider)
	require.True(t, ok)

	models, err := catalogProvider.Models(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "ollama/llama3.2:3b", models[0].ID)
	assert.Equal(t, "llama3.2:3b", models[0].UpstreamID)
	assert.Equal(t, "llama", models[0].Architecture.Tokenizer)
	assert.Equal(t, "auto", models[0].Source)
}

func TestAdapter_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/version", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.ProviderConfig{
		ID:      "ollama",
		Type:    "ollama",
		BaseURL: server.URL + "/v1",
	})
	require.NoError(t, err)
	require.NoError(t, adapter.Health(context.Background()))
}

func TestAdapter_Models_RespectsExcludePatterns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "cringe:latest", "model": "cringe:latest"},
				{"name": "small_cringe:latest", "model": "small_cringe:latest"},
			},
		})
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.ProviderConfig{
		ID:      "ollama",
		Type:    "ollama",
		BaseURL: server.URL + "/v1",
		Config: map[string]string{
			"exclude_name_patterns": "small_*",
		},
	})
	require.NoError(t, err)

	catalogProvider := adapter.(llm.ModelCatalogProvider)
	models, err := catalogProvider.Models(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "ollama/cringe:latest", models[0].ID)
}

func TestInferTokenizer(t *testing.T) {
	assert.Equal(t, "qwen2", inferTokenizer(&modelDetails{Family: "qwen2"}))
	assert.Equal(t, "phi2", inferTokenizer(&modelDetails{Family: "phi3"}))
	assert.Equal(t, "llama", inferTokenizer(nil))
}
