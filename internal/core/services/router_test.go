package services

import (
	"context"
	"testing"

	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockProvider implements ports.ModelProvider for testing
type MockProvider struct {
	mock.Mock
	ID           string
	ProviderType string
}

func (m *MockProvider) Name() string { return m.ID }
func (m *MockProvider) Type() string { return m.ProviderType }

func (m *MockProvider) Models(ctx context.Context) ([]schema.Model, error) {
	// Not used in new router logic
	return nil, nil
}

func (m *MockProvider) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.ChatResponse), args.Error(1)
}

func (m *MockProvider) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(<-chan ports.StreamResult), args.Error(1)
}

func TestListAllModels_RegistryBased(t *testing.T) {
	// Setup Registry
	defs := []schema.ModelDefinition{
		{
			ID:         "gpt-4",
			Name:       "GPT-4",
			ProviderID: "p1",
			UpstreamID: "gpt-4-0613",
			Enabled:    true,
			Config: schema.ModelConfig{
				Modality: []string{"text"},
			},
		},
		{
			ID:         "claude-3",
			Name:       "Claude 3",
			ProviderID: "p2",
			UpstreamID: "claude-3-opus",
			Enabled:    true,
			Config: schema.ModelConfig{
				Modality: []string{"text", "image"},
			},
		},
	}
	registry := NewInMemoryModelRegistry(defs)

	// Setup Router
	router := NewRouterService(registry, nil)

	ctx := context.Background()

	// Test 1: No Filter
	models, err := router.ListAllModels(ctx, ports.ModelFilter{})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(models))

	// Test 2: Filter by Provider
	models, err = router.ListAllModels(ctx, ports.ModelFilter{Provider: "p1"})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(models))
	assert.Equal(t, "gpt-4", models[0].ID)

	// Test 3: Filter by ID
	models, err = router.ListAllModels(ctx, ports.ModelFilter{ID: "claude"})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(models))
	assert.Equal(t, "claude-3", models[0].ID)
}

func TestChat_Routing(t *testing.T) {
	// Setup Registry
	defs := []schema.ModelDefinition{
		{
			ID:         "my-gpt",
			ProviderID: "openai-main",
			UpstreamID: "gpt-4-upstream",
			Enabled:    true,
		},
	}
	registry := NewInMemoryModelRegistry(defs)

	// Setup Provider Mock
	mockProvider := new(MockProvider)
	mockProvider.ID = "openai-main"
	mockProvider.ProviderType = "openai"

	mockProvider.On("Chat", mock.Anything, mock.MatchedBy(func(req *schema.ChatRequest) bool {
		// Verify that the router translated "my-gpt" to "gpt-4-upstream"
		return req.Model == "gpt-4-upstream"
	})).Return(&schema.ChatResponse{}, nil)

	// Setup Router
	router := NewRouterService(registry, nil)
	router.RegisterProvider(mockProvider)

	// Execute Chat
	req := &schema.ChatRequest{
		Model: "my-gpt",
		Messages: []schema.ChatMessage{
			{Role: "user", Content: schema.Content{Text: "Hello"}},
		},
	}

	_, err := router.Chat(context.Background(), req)
	assert.NoError(t, err)

	mockProvider.AssertExpectations(t)
}

func TestChat_Failures(t *testing.T) {
	registry := NewInMemoryModelRegistry(nil)
	router := NewRouterService(registry, nil)

	// Test Unknown Model
	_, err := router.Chat(context.Background(), &schema.ChatRequest{Model: "unknown"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model not found")
}