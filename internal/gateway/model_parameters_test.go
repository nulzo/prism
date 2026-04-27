package gateway

import (
	"context"
	"testing"

	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

func TestSanitizeRequestForModelDropsUnsupportedReasoning(t *testing.T) {
	enabled := true
	includeReasoning := true
	req := &api.ChatRequest{
		Model:            "openai/gpt-4.1",
		Messages:         []api.ChatMessage{{Role: "user", Content: api.Content{Text: "hi"}}},
		Reasoning:        &api.ReasoningConfig{Enabled: &enabled, Effort: "medium"},
		IncludeReasoning: &includeReasoning,
	}

	sanitizeRequestForModel(req, api.ModelDefinition{
		ID:                  "openai/gpt-4.1",
		SupportedParameters: []string{"temperature", "max_tokens"},
	})

	if req.Reasoning != nil {
		t.Fatalf("expected unsupported reasoning config to be stripped, got %+v", req.Reasoning)
	}
	if req.IncludeReasoning != nil {
		t.Fatalf("expected unsupported include_reasoning to be stripped")
	}
}

func TestSanitizeRequestForModelKeepsSupportedReasoning(t *testing.T) {
	enabled := true
	req := &api.ChatRequest{
		Model:     "openai/gpt-5",
		Messages:  []api.ChatMessage{{Role: "user", Content: api.Content{Text: "hi"}}},
		Reasoning: &api.ReasoningConfig{Enabled: &enabled, Effort: "high"},
	}

	sanitizeRequestForModel(req, api.ModelDefinition{
		ID:                  "openai/gpt-5",
		SupportedParameters: []string{"temperature", "reasoning"},
	})

	if req.Reasoning == nil {
		t.Fatalf("expected supported reasoning config to be preserved")
	}
}

func TestServiceChatAppliesModelParameterPolicy(t *testing.T) {
	enabled := true
	provider := &capturingProvider{name: "mock-provider"}
	svc := NewService(zap.NewNop(), &parameterPolicyRepo{}, &parameterPolicyIngestor{}, nil)
	if err := svc.RegisterProvider(context.Background(), provider, []api.ModelDefinition{{
		ID:                  "mock/model",
		ProviderID:          "mock-provider",
		UpstreamID:          "model",
		SupportedParameters: []string{"temperature"},
	}}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	_, err := svc.Chat(context.Background(), &api.ChatRequest{
		Model:     "mock/model",
		Messages:  []api.ChatMessage{{Role: "user", Content: api.Content{Text: "hi"}}},
		Reasoning: &api.ReasoningConfig{Enabled: &enabled, Effort: "medium"},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	if provider.chatReq == nil {
		t.Fatalf("expected provider request to be captured")
	}
	if provider.chatReq.Reasoning != nil {
		t.Fatalf("expected provider request reasoning to be stripped, got %+v", provider.chatReq.Reasoning)
	}
}

type capturingProvider struct {
	name    string
	chatReq *api.UpstreamChatRequest
}

func (p *capturingProvider) Name() string { return p.name }
func (p *capturingProvider) Type() string { return "mock" }
func (p *capturingProvider) Health(ctx context.Context) error {
	return nil
}
func (p *capturingProvider) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	p.chatReq = req
	return &api.ChatResponse{
		ID: "chatcmpl-test",
		Choices: []api.Choice{{
			Message:      &api.ChatMessage{Role: "assistant", Content: api.Content{Text: "ok"}},
			FinishReason: "stop",
		}},
	}, nil
}
func (p *capturingProvider) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)
	close(ch)
	return ch, nil
}

type parameterPolicyIngestor struct{}

func (parameterPolicyIngestor) Log(log *model.RequestLog) {}
func (parameterPolicyIngestor) Start(ctx context.Context) {}
func (parameterPolicyIngestor) Stop()                     {}

type parameterPolicyRepo struct{}

func (parameterPolicyRepo) APIKeys() store.APIKeyRepository     { return nil }
func (parameterPolicyRepo) Requests() store.RequestRepository   { return nil }
func (parameterPolicyRepo) Providers() store.ProviderRepository { return parameterPolicyProviderRepo{} }
func (parameterPolicyRepo) Users() store.UserRepository         { return nil }
func (parameterPolicyRepo) Audit() store.AuditRepository        { return nil }
func (parameterPolicyRepo) WithTx(ctx context.Context, fn func(repo store.Repository) error) error {
	return fn(parameterPolicyRepo{})
}
func (parameterPolicyRepo) Close() error { return nil }

type parameterPolicyProviderRepo struct{}

func (parameterPolicyProviderRepo) ListActive(ctx context.Context) ([]model.Provider, error) {
	return nil, nil
}
func (parameterPolicyProviderRepo) GetModelPricing(ctx context.Context, modelID string) (*model.Model, error) {
	return nil, nil
}
func (parameterPolicyProviderRepo) SyncModels(ctx context.Context, models []model.Model) error {
	return nil
}
func (parameterPolicyProviderRepo) SyncProviders(ctx context.Context, providers []model.Provider) error {
	return nil
}
