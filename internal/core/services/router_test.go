package services

import (
	"context"
	"testing"

	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
)

// MockProvider implements ports.ModelProvider for testing
type MockProvider struct {
	ID           string
	ProviderType string
	ModelsList   []schema.Model
	ShouldFail   bool
}

func (m *MockProvider) Name() string { return m.ID }
func (m *MockProvider) Type() string { return m.ProviderType }

func (m *MockProvider) Models(ctx context.Context) ([]schema.Model, error) {
	if m.ShouldFail {
		return nil, assert.AnError
	}
	return m.ModelsList, nil
}

// Stubs for other methods
func (m *MockProvider) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	return nil, nil
}
func (m *MockProvider) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	return nil, nil
}

func TestListAllModels_Filtering(t *testing.T) {
	// Setup Providers
	p1 := &MockProvider{
		ID:           "p1",
		ProviderType: "openai",
		ModelsList: []schema.Model{
			{ID: "gpt-4", Provider: "p1", Architecture: schema.Architecture{Modality: "text"}},
		},
	}
	p2 := &MockProvider{
		ID:           "p2",
		ProviderType: "anthropic",
		ModelsList: []schema.Model{
			{ID: "claude-3", Provider: "p2", Architecture: schema.Architecture{Modality: "text"}},
		},
	}
	p3 := &MockProvider{
		ID:           "p3",
		ProviderType: "ollama",
		ModelsList: []schema.Model{
			{ID: "llama3", Provider: "p3", Architecture: schema.Architecture{Modality: "text"}},
		},
	}

	// Setup Router
	router := NewRouterService(nil) // No cache needed for this test
	router.RegisterProvider(p1)
	router.RegisterProvider(p2)
	router.RegisterProvider(p3)

	ctx := context.Background()

	tests := []struct {
		name          string
		filter        ports.ModelFilter
		expectedCount int
		expectedIDs   []string
	}{
		{
			name:          "No Filter",
			filter:        ports.ModelFilter{},
			expectedCount: 3,
			expectedIDs:   []string{"gpt-4", "claude-3", "llama3"},
		},
		{
			name:          "Filter by Provider Type (ollama)",
			filter:        ports.ModelFilter{Provider: "ollama"},
			expectedCount: 1,
			expectedIDs:   []string{"llama3"},
		},
		{
			name:          "Filter by Provider ID (p1)",
			filter:        ports.ModelFilter{Provider: "p1"},
			expectedCount: 1,
			expectedIDs:   []string{"gpt-4"},
		},
		{
			name:          "Filter by Model ID (partial)",
			filter:        ports.ModelFilter{ID: "gpt"},
			expectedCount: 1,
			expectedIDs:   []string{"gpt-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, err := router.ListAllModels(ctx, tt.filter)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(models))

			// Verify IDs
			foundIDs := make(map[string]bool)
			for _, m := range models {
				foundIDs[m.ID] = true
			}
			for _, id := range tt.expectedIDs {
				assert.True(t, foundIDs[id], "Expected model %s not found", id)
			}
		})
	}
}

func TestListAllModels_ProviderSelectionOptimization(t *testing.T) {
	// This test verifies that if we filter by provider, we ONLY call that provider

	p1 := &MockProvider{ID: "p1", ProviderType: "type1", ModelsList: []schema.Model{{ID: "m1"}}}
	p2 := &MockProvider{ID: "p2", ProviderType: "type2", ModelsList: []schema.Model{{ID: "m2"}}}

	router := NewRouterService(nil)
	router.RegisterProvider(p1)
	router.RegisterProvider(p2)

	ctx := context.Background()

	// Filter for p1
	models, err := router.ListAllModels(ctx, ports.ModelFilter{Provider: "p1"})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(models))
	assert.Equal(t, "m1", models[0].ID)

	p2_sneaky := &MockProvider{
		ID:           "p2",
		ProviderType: "type2",
		ModelsList:   []schema.Model{{ID: "m2", Provider: "p1"}},
	}

	router2 := NewRouterService(nil)
	router2.RegisterProvider(p1)
	router2.RegisterProvider(p2_sneaky)

	models2, err := router2.ListAllModels(ctx, ports.ModelFilter{Provider: "p1"})
	assert.NoError(t, err)

	assert.Equal(t, 1, len(models2), "Should only fetch from requested provider, ignoring others even if they could return matching data")
	assert.Equal(t, "m1", models2[0].ID)
}

func TestListAllModels_Cache(t *testing.T) {
	// TODO: Add cache test
}
