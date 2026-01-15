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
	ID          string
	ProviderType string
	ModelsList  []schema.Model
	ShouldFail  bool
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
	
	// If we filter for p1, p2 should NOT be called.
	// To verify this, we can make p2 fail. If ListAllModels doesn't error or log/call it, we are good.
	// But ListAllModels suppresses errors. 
	// So let's make p2 return a unique model. If it's in the list, p2 was called.
	
	router := NewRouterService(nil)
	router.RegisterProvider(p1)
	router.RegisterProvider(p2)

	ctx := context.Background()
	
	// Filter for p1
	models, err := router.ListAllModels(ctx, ports.ModelFilter{Provider: "p1"})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(models))
	assert.Equal(t, "m1", models[0].ID)
	
	// If p2 was called, it would have returned "m2", and since we clear the filter after selection,
	// applyFilters would have kept it IF it was in the list.
	// Wait, if p2 WAS called, it would return "m2". 
	// The effective filter has Provider cleared. 
	// So "m2" WOULD be in the result if p2 was called.
	// So checking that "m2" is NOT present proves p2 was not called (or filtered out early).
	
	// Double check: if we ask for p1, and logic iterates ALL providers:
	// p1 -> m1
	// p2 -> m2
	// Filter: Provider=p1.
	// applyFilters checks m.Provider vs p1.
	// m1.Provider = "p1". match.
	// m2.Provider = "p2". no match.
	// So even without optimization, result is correct.
	
	// To prove optimization, we need a side effect.
	// Let's use a flag or a panic in the mock?
	// But strict dependency injection of mocks is limited here.
	
	// Let's make p2 VERY slow. If test finishes fast, optimization works? Flaky.
	// Let's make p2 fail with error. Router logs warning.
	// We can't easily check logs.
	
	// Actually, the best way:
	// p2 returns a model "m2" with Provider="p1" (fake it).
	// If we iterate all, p2 returns m2(Provider=p1).
	// Filter matches p1. m2 is kept.
	// Result has m1, m2.
	// If we optimize, p2 is never called.
	// Result has m1.
	
	p2_sneaky := &MockProvider{
		ID: "p2", 
		ProviderType: "type2", 
		ModelsList: []schema.Model{{ID: "m2", Provider: "p1"}}, // Claims to be p1
	}
	
	router2 := NewRouterService(nil)
	router2.RegisterProvider(p1)
	router2.RegisterProvider(p2_sneaky)
	
	models2, err := router2.ListAllModels(ctx, ports.ModelFilter{Provider: "p1"})
	assert.NoError(t, err)
	
	// If optimization works, p2_sneaky is NOT called. only m1 returned.
	// If optimization fails (calls all), p2_sneaky returns m2. Filter checks Provider="p1". m2 has "p1". Kept.
	// Result would be 2 models.
	
	assert.Equal(t, 1, len(models2), "Should only fetch from requested provider, ignoring others even if they could return matching data")
	assert.Equal(t, "m1", models2[0].ID)
}

func TestListAllModels_Cache(t *testing.T) {
	// TODO: Add cache test
}
