package gateway

import (
	"context"
	"testing"

	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

// mockProviderImpl is a simple provider for testing
type mockProviderImpl struct {
	name string
}

func (m *mockProviderImpl) Name() string { return m.name }
func (m *mockProviderImpl) Type() string { return "mock" }
func (m *mockProviderImpl) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	return nil, nil
}

func (m *mockProviderImpl) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	return nil, nil
}

func (m *mockProviderImpl) Health(ctx context.Context) error {
	return nil
}

func TestService_Models(t *testing.T) {
	log := zap.NewNop()
	svc := NewService(log, nil, nil, nil)

	p := &mockProviderImpl{name: "mock-provider"}

	models := []api.ModelDefinition{
		{
			ID:         "mock/model-1",
			ProviderID: "mock-provider",
			UpstreamID: "model-1",
			Name:       "Mock Model 1",
			Enabled:    true,
			Pricing: api.ModelPricing{
				Prompt: "0.1",
			},
		},
		{
			ID:         "mock/model-2",
			ProviderID: "mock-provider",
			UpstreamID: "model-2",
			Name:       "Mock Model 2",
			Enabled:    true,
		},
	}

	// Test RegisterProvider
	err := svc.RegisterProvider(context.Background(), p, models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test ListAllModels without filter
	allModels, err := svc.ListAllModels(context.Background(), api.ModelFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(allModels) != 2 {
		t.Fatalf("expected 2 models, got %d", len(allModels))
	}

	// Test ListAllModels with filter
	filteredModels, err := svc.ListAllModels(context.Background(), api.ModelFilter{Provider: "non-existent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filteredModels) != 0 {
		t.Fatalf("expected 0 models, got %d", len(filteredModels))
	}

	filteredModels, err = svc.ListAllModels(context.Background(), api.ModelFilter{Provider: "mock-provider"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filteredModels) != 2 {
		t.Fatalf("expected 2 models, got %d", len(filteredModels))
	}

	// Test GetProviderForModel
	prov, upstreamID, err := svc.GetProviderForModel(context.Background(), "mock/model-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.Name() != "mock-provider" {
		t.Fatalf("expected provider mock-provider, got %s", prov.Name())
	}
	if upstreamID != "model-1" {
		t.Fatalf("expected upstream id model-1, got %s", upstreamID)
	}

	// Test GetProviderForModel fallback upstream ID
	_, upstreamID, err = svc.GetProviderForModel(context.Background(), "mock/model-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upstreamID != "model-2" {
		t.Fatalf("expected upstream id model-2, got %s", upstreamID)
	}
}

func TestService_PermissivePassThrough(t *testing.T) {
	log := zap.NewNop()
	svc := NewService(log, nil, nil, nil)

	p := &mockProviderImpl{name: "mock-provider"}

	err := svc.RegisterProvider(context.Background(), p, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test GetProviderForModel with a model not in config, but prefix matches provider
	prov, upstreamID, err := svc.GetProviderForModel(context.Background(), "mock-provider/dynamic-model-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.Name() != "mock-provider" {
		t.Fatalf("expected provider mock-provider, got %s", prov.Name())
	}
	if upstreamID != "dynamic-model-123" {
		t.Fatalf("expected upstream id dynamic-model-123, got %s", upstreamID)
	}

	// Test GetProviderForModel with a non-existent provider prefix
	_, _, err = svc.GetProviderForModel(context.Background(), "unknown/dynamic-model-123")
	if err == nil {
		t.Fatalf("expected error for unknown provider prefix, got nil")
	}
}
