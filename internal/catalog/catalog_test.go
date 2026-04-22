package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a minimal llm.Provider stub scoped to this test file.
type fakeProvider struct {
	name   string
	models []api.ModelDefinition
	err    error
	calls  int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Type() string { return "fake" }
func (f *fakeProvider) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.models, nil
}
func (f *fakeProvider) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeProvider) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeProvider) Health(ctx context.Context) error    { return nil }
func (f *fakeProvider) Config() config.ProviderConfig       { return config.ProviderConfig{ID: f.name} }
func (f *fakeProvider) Capabilities() llm.Capabilities      { return llm.Capabilities{} }

// --- tests ---------------------------------------------------------------

func TestCatalog_HydrateMergesStaticAndUpstream(t *testing.T) {
	static := []api.ModelDefinition{{
		ID:            "openai/gpt-4o-mini",
		ProviderID:    "openai-main",
		Name:          "GPT-4o Mini (curated name)",
		Description:   "Curated description",
		Pricing:       api.ModelPricing{Prompt: "0.15", Completion: "0.6"},
		ContextLength: 0, // upstream wins when static is zero
	}}
	upstream := []api.ModelDefinition{{
		ID:            "openai/gpt-4o-mini",
		ProviderID:    "openai-main",
		UpstreamID:    "gpt-4o-mini",
		Name:          "upstream-name-that-should-be-overridden",
		Description:   "upstream desc that should be overridden",
		ContextLength: 128000,
		Architecture: api.ModelArchitecture{
			InputModalities:  []string{"text", "image"},
			OutputModalities: []string{"text"},
		},
	}}

	c := New(Options{Static: static})
	c.Add(&fakeProvider{name: "openai-main", models: upstream})

	res, err := c.Hydrate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, res.TotalModels)
	// Note: the static entry is already in the seed snapshot so a hydrate
	// that only reconfirms it produces zero "added" and (likely) zero
	// "updated" diffs — the interesting assertion is that the merged
	// record below reflects both sources.
	_ = res

	entry, ok := c.Lookup("openai/gpt-4o-mini")
	require.True(t, ok)
	assert.Equal(t, "GPT-4o Mini (curated name)", entry.Name, "static name wins over upstream")
	assert.Equal(t, "Curated description", entry.Description, "static description wins")
	assert.Equal(t, "0.15", entry.Pricing.Prompt, "static pricing wins")
	assert.Equal(t, 128000, entry.ContextLength, "upstream context length fills in when static is zero")
	assert.Equal(t, []string{"text", "image"}, entry.Architecture.InputModalities)
	assert.Equal(t, SourceMerged, entry.Provenance)
}

func TestCatalog_HydrateDiscoversUpstreamOnlyModels(t *testing.T) {
	upstream := []api.ModelDefinition{{
		ID:            "anthropic/claude-4.5-sonnet",
		ProviderID:    "anthropic-main",
		UpstreamID:    "claude-4-5-sonnet-latest",
		ContextLength: 200000,
	}}

	c := New(Options{})
	c.Add(&fakeProvider{name: "anthropic-main", models: upstream})

	_, err := c.Hydrate(context.Background())
	require.NoError(t, err)

	provID, upID, ok := c.Resolve("anthropic/claude-4.5-sonnet")
	require.True(t, ok)
	assert.Equal(t, "anthropic-main", provID)
	assert.Equal(t, "claude-4-5-sonnet-latest", upID)

	entry, ok := c.Lookup("anthropic/claude-4.5-sonnet")
	require.True(t, ok)
	assert.Equal(t, SourceUpstream, entry.Provenance)
}

func TestCatalog_HydrateRespectsPerProviderScope(t *testing.T) {
	providerA := &fakeProvider{name: "a", models: []api.ModelDefinition{{ID: "a/m1", ProviderID: "a"}}}
	providerB := &fakeProvider{name: "b", models: []api.ModelDefinition{{ID: "b/m1", ProviderID: "b"}}}

	c := New(Options{})
	c.Add(providerA)
	c.Add(providerB)

	_, err := c.Hydrate(context.Background())
	require.NoError(t, err)
	assert.Len(t, c.All(), 2)

	providerA.models = []api.ModelDefinition{
		{ID: "a/m1", ProviderID: "a"},
		{ID: "a/m2", ProviderID: "a"},
	}
	providerB.models = nil // should be ignored — hydrate is scoped to "a"

	_, err = c.Hydrate(context.Background(), "a")
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, e := range c.All() {
		ids[e.ID] = true
	}
	assert.True(t, ids["a/m1"])
	assert.True(t, ids["a/m2"])
	assert.True(t, ids["b/m1"], "provider b's entry should be preserved across a scoped hydrate")
}

func TestCatalog_HydrateSurfacesProviderErrors(t *testing.T) {
	good := &fakeProvider{name: "good", models: []api.ModelDefinition{{ID: "good/m", ProviderID: "good"}}}
	bad := &fakeProvider{name: "bad", err: errors.New("boom")}

	c := New(Options{HydrateTimeout: 200 * time.Millisecond})
	c.Add(good)
	c.Add(bad)

	res, err := c.Hydrate(context.Background())
	require.NoError(t, err, "one failing provider must not break the whole hydrate")

	assert.Equal(t, 1, res.TotalModels)
	assert.NoError(t, res.Providers["good"].Err)
	require.Error(t, res.Providers["bad"].Err)
}

func TestCatalog_FilterByProvider(t *testing.T) {
	c := New(Options{Static: []api.ModelDefinition{
		{ID: "a/m1", ProviderID: "a"},
		{ID: "b/m1", ProviderID: "b"},
	}})

	out := c.Filter(api.ModelFilter{Provider: "a"})
	require.Len(t, out, 1)
	assert.Equal(t, "a/m1", out[0].ID)
}
