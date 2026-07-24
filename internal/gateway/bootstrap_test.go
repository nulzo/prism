package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	_ "github.com/nulzo/model-router-api/internal/llm/ollama"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBootstrapProviders_DiscoversOllamaModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama3:latest", "model": "llama3:latest"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	log := zap.NewNop()
	svc := NewService(log, nil, nil, nil)

	count := BootstrapProviders(context.Background(), svc, []config.ProviderConfig{
		{
			ID:           "ollama",
			Type:         "ollama",
			Name:         "Ollama",
			BaseURL:      server.URL,
			Enabled:      true,
			RequiresAuth: false,
			Config:       map[string]string{"discover_models": "true"},
		},
	}, log)
	require.Equal(t, 1, count)

	models, err := svc.ListAllModels(context.Background(), api.ModelFilter{Provider: "ollama"})
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "ollama/llama3:latest", models[0].ID)
}

func TestRefreshCatalog_ReplacesProviderModels(t *testing.T) {
	var tagPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(tagPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	log := zap.NewNop()
	svc := NewService(log, nil, nil, nil)

	tagPayload = map[string]interface{}{
		"models": []map[string]interface{}{
			{"name": "llama3:latest", "model": "llama3:latest"},
		},
	}

	count := BootstrapProviders(context.Background(), svc, []config.ProviderConfig{
		{
			ID:           "ollama",
			Type:         "ollama",
			Name:         "Ollama",
			BaseURL:      server.URL,
			Enabled:      true,
			RequiresAuth: false,
		},
	}, log)
	require.Equal(t, 1, count)

	tagPayload = map[string]interface{}{
		"models": []map[string]interface{}{
			{"name": "phi3:latest", "model": "phi3:latest"},
		},
	}

	refresher := svc
	err := refresher.RefreshCatalog(context.Background(), []config.ProviderConfig{
		{ID: "ollama", Enabled: true},
	}, time.Second)
	require.NoError(t, err)

	models, err := svc.ListAllModels(context.Background(), api.ModelFilter{Provider: "ollama"})
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "ollama/phi3:latest", models[0].ID)
}
