package catalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nulzo/model-router-api/internal/catalog"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCatalogProvider struct {
	models []api.ModelDefinition
	err    error
}

func (m *mockCatalogProvider) Name() string { return "mock" }
func (m *mockCatalogProvider) Type() string { return "mock" }
func (m *mockCatalogProvider) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	return nil, nil
}
func (m *mockCatalogProvider) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	return nil, nil
}
func (m *mockCatalogProvider) Health(ctx context.Context) error { return nil }
func (m *mockCatalogProvider) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	return m.models, m.err
}

type plainProvider struct{}

func (plainProvider) Name() string { return "plain" }
func (plainProvider) Type() string { return "plain" }
func (plainProvider) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	return nil, nil
}
func (plainProvider) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	return nil, nil
}
func (plainProvider) Health(ctx context.Context) error { return nil }

func TestDiscoveryEnabled(t *testing.T) {
	assert.True(t, catalog.DiscoveryEnabled(config.ProviderConfig{}))
	assert.False(t, catalog.DiscoveryEnabled(config.ProviderConfig{
		Config: map[string]string{"discover_models": "false"},
	}))
}

func TestResolveProviderModels_DiscoversAndMerges(t *testing.T) {
	provider := &mockCatalogProvider{
		models: []api.ModelDefinition{
			{ID: "ollama/llama3:latest", ProviderID: "ollama", UpstreamID: "llama3:latest", Enabled: true},
		},
	}
	static := []api.ModelDefinition{
		{ID: "ollama/llama3:latest", ProviderID: "ollama", UpstreamID: "llama3:latest", Enabled: true, ContextLength: 4096},
	}
	cfg := config.ProviderConfig{ID: "ollama", Config: map[string]string{"discover_models": "true"}}

	models, err := catalog.ResolveProviderModels(context.Background(), provider, static, cfg, time.Second)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, 4096, models[0].ContextLength)
}

func TestResolveProviderModels_FallsBackToStaticOnDiscoveryError(t *testing.T) {
	provider := &mockCatalogProvider{err: errors.New("upstream down")}
	static := []api.ModelDefinition{
		{ID: "ollama/fallback:latest", ProviderID: "ollama", UpstreamID: "fallback:latest", Enabled: true},
	}

	models, err := catalog.ResolveProviderModels(context.Background(), provider, static, config.ProviderConfig{ID: "ollama"}, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "ollama/fallback:latest", models[0].ID)
}

func TestResolveProviderModels_ReturnsErrorWhenDiscoveryRequired(t *testing.T) {
	provider := &mockCatalogProvider{err: errors.New("upstream down")}

	_, err := catalog.ResolveProviderModels(context.Background(), provider, nil, config.ProviderConfig{ID: "ollama"}, time.Second)
	require.Error(t, err)
}

func TestResolveProviderModels_SkipsDiscoveryWhenDisabled(t *testing.T) {
	var _ llm.ModelCatalogProvider = (*mockCatalogProvider)(nil)

	models, err := catalog.ResolveProviderModels(
		context.Background(),
		&mockCatalogProvider{models: []api.ModelDefinition{{ID: "ollama/new:latest", Enabled: true}}},
		[]api.ModelDefinition{{ID: "ollama/static:latest", Enabled: true}},
		config.ProviderConfig{Config: map[string]string{"discover_models": "false"}},
		time.Second,
	)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "ollama/static:latest", models[0].ID)
}

func TestResolveProviderModels_PlainProviderUsesStaticOnly(t *testing.T) {
	models, err := catalog.ResolveProviderModels(
		context.Background(),
		plainProvider{},
		[]api.ModelDefinition{{ID: "openai/gpt-4", Enabled: true}},
		config.ProviderConfig{ID: "openai"},
		time.Second,
	)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "openai/gpt-4", models[0].ID)
}
