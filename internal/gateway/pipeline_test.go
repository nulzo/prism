package gateway

import (
	"context"
	"testing"

	"github.com/nulzo/model-router-api/internal/extension"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/plugin"
	"github.com/nulzo/model-router-api/pkg/api"
)

// mockProvider implements llm.Provider for testing
type mockProvider struct {
	chatFunc func(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error)
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Type() string { return "mock" }
func (m *mockProvider) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	return m.chatFunc(ctx, req)
}
func (m *mockProvider) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	return nil, nil
}
func (m *mockProvider) Models(ctx context.Context) ([]api.ModelDefinition, error) { return nil, nil }
func (m *mockProvider) Health(ctx context.Context) error                          { return nil }
func (m *mockProvider) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat}
}

func TestPipelineOrchestrator_Execute(t *testing.T) {
	pReg := plugin.NewRegistry()
	pReg.Register(plugin.NewContextCompressionPlugin(5))

	eReg := extension.NewRegistry()
	eReg.Register(extension.NewDatetimeExtension())

	orchestrator := NewPipelineOrchestrator(pReg, eReg)

	// We'll track how many times Chat is called to verify the agentic loop
	callCount := 0

	mockProv := &mockProvider{
		chatFunc: func(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				if len(req.Tools) != 1 || req.Tools[0].Function.Name != "prism:datetime" {
					t.Fatalf("expected exactly one injected extension tool prism:datetime, got %+v", req.Tools)
				}
			}
			if callCount == 2 {
				if len(req.Messages) != 3 {
					t.Fatalf("expected 3 upstream messages on second iteration, got %d", len(req.Messages))
				}
				if len(req.Messages[1].ToolCalls) != 1 || req.Messages[1].ToolCalls[0].ExtraContent == nil || req.Messages[1].ToolCalls[0].ExtraContent.Google == nil || req.Messages[1].ToolCalls[0].ExtraContent.Google.ThoughtSignature != "sig-123" {
					t.Fatalf("expected thought signature to be preserved on upstream continuation, got %+v", req.Messages[1].ToolCalls)
				}
			}

			// First call: model decides to use the datetime extension
			if callCount == 1 {
				return &api.ChatResponse{
					Choices: []api.Choice{
						{
							FinishReason: "tool_calls",
							Message: &api.ChatMessage{
								Role: "assistant",
								ToolCalls: []api.ToolCall{
									{
										ID:   "call_123",
										Type: "function",
										Function: api.FunctionCall{
											Name:      "prism:datetime",
											Arguments: `{"timezone": "UTC"}`,
										},
										ExtraContent: &api.ToolExtraContent{
											Google: &api.GoogleToolExtraContent{
												ThoughtSignature: "sig-123",
											},
										},
									},
								},
							},
						},
					},
				}, nil
			}

			// Second call: model returns final answer
			return &api.ChatResponse{
				Choices: []api.Choice{
					{
						FinishReason: "stop",
						Message: &api.ChatMessage{
							Role:    "assistant",
							Content: api.Content{Text: "The time is now."},
						},
					},
				},
			}, nil
		},
	}

	req := &api.ChatRequest{
		Model: "mock-model",
		Messages: []api.ChatMessage{
			{Role: "user", Content: api.Content{Text: "What time is it?"}},
		},
		Plugins: []api.PluginConfig{
			{ID: "context-compression"},
		},
		Extensions: []api.ExtensionConfig{
			{ID: "prism:datetime"},
		},
	}

	resp, err := orchestrator.Execute(context.Background(), req, mockProv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls to provider (agentic loop), got %d", callCount)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content.Text != "The time is now." {
		t.Errorf("unexpected final response: %+v", resp)
	}

	// The public request should not be mutated by orchestration; provider-facing
	// state is maintained in a sanitized upstream request.
	if len(req.Messages) != 1 {
		t.Errorf("expected original request to remain unchanged, got %d messages", len(req.Messages))
	}
}
