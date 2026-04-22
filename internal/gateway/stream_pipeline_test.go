package gateway

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nulzo/model-router-api/internal/extension"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/plugin"
	"github.com/nulzo/model-router-api/pkg/api"
)

// streamingMockProvider scripts a sequence of upstream streams. Each call to
// Stream() returns the next scripted stream so we can simulate the agentic
// loop's "first iteration: tool_calls; second iteration: final answer" flow.
type streamingMockProvider struct {
	streams [][]api.StreamResult
	calls   atomic.Int32
	caps    llm.Capabilities
}

func (m *streamingMockProvider) Name() string                         { return "mock" }
func (m *streamingMockProvider) Type() string                         { return "mock" }
func (m *streamingMockProvider) Capabilities() llm.Capabilities       { return m.caps }
func (m *streamingMockProvider) Models(ctx context.Context) ([]api.ModelDefinition, error) { return nil, nil }
func (m *streamingMockProvider) Health(ctx context.Context) error     { return nil }

func (m *streamingMockProvider) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	return nil, nil
}

func (m *streamingMockProvider) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	idx := int(m.calls.Add(1)) - 1
	if idx >= len(m.streams) {
		idx = len(m.streams) - 1
	}
	chunks := m.streams[idx]
	out := make(chan api.StreamResult, len(chunks))
	go func() {
		defer close(out)
		for _, c := range chunks {
			out <- c
		}
	}()
	return out, nil
}

// TestPipeline_StreamWithExtension_AgenticLoop verifies that:
//   - the gateway transparently runs the agentic loop in streaming mode
//   - the trailing tool_calls finish + usage chunks of an intermediate
//     iteration are suppressed (client never sees a "false done")
//   - the final iteration's stop + usage chunks ARE forwarded
//   - the final concatenated content matches what the second iteration emitted
//   - synthetic prism.tool_event chunks are emitted for UI visibility
func TestPipeline_StreamWithExtension_AgenticLoop(t *testing.T) {
	pReg := plugin.NewRegistry()
	eReg := extension.NewRegistry()
	eReg.Register(extension.NewDatetimeExtension())

	mock := &streamingMockProvider{
		caps: llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat},
		streams: [][]api.StreamResult{
			// Iteration 1: model decides to call prism:datetime.
			{
				{Response: &api.ChatResponse{
					ID: "gen-1", Model: "mock-model",
					Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{
						Role: "assistant",
						ToolCalls: []api.ToolCall{{
							ID: "call_1", Type: "function",
							Function: api.FunctionCall{
								Name:      "prism_datetime",
								Arguments: `{"timezone":"UTC"}`,
							},
						}},
					}}},
				}},
				// Empty finish chunk — must be suppressed in this iteration.
				{Response: &api.ChatResponse{
					Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{}, FinishReason: "tool_calls"}},
				}},
				// Usage-only chunk — must also be suppressed.
				{Response: &api.ChatResponse{
					Usage: &api.ResponseUsage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
				}},
			},
			// Iteration 2: model returns the final answer.
			{
				{Response: &api.ChatResponse{
					Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{Role: "assistant", Content: api.Content{Text: "It is "}}}},
				}},
				{Response: &api.ChatResponse{
					Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{Content: api.Content{Text: "now."}}}},
				}},
				{Response: &api.ChatResponse{
					Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{}, FinishReason: "stop"}},
				}},
				{Response: &api.ChatResponse{
					Usage: &api.ResponseUsage{PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18},
				}},
			},
		},
	}

	orch := NewPipelineOrchestrator(pReg, eReg)
	req := &api.ChatRequest{
		Model: "mock-model",
		Messages: []api.ChatMessage{
			{Role: "user", Content: api.Content{Text: "what time is it?"}},
		},
		Extensions: []api.ExtensionConfig{{ID: "prism:datetime"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := orch.Stream(ctx, req, mock)
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	var (
		text             strings.Builder
		sawToolCallEvent bool
		sawToolResultEv  bool
		sawStopFinish    bool
		sawSuspectFinish bool
		sawFinalUsage    bool
		chunkCount       int
	)
	for r := range stream {
		if r.Err != nil {
			t.Fatalf("stream emitted error: %v", r.Err)
		}
		chunkCount++
		resp := r.Response
		if resp == nil {
			continue
		}
		if resp.Usage != nil && resp.Usage.PromptTokens == 12 {
			sawFinalUsage = true
		}
		for _, ch := range resp.Choices {
			if ch.FinishReason == "stop" {
				sawStopFinish = true
			}
			if ch.FinishReason == "tool_calls" {
				sawSuspectFinish = true
			}
			if ch.Delta != nil {
				text.WriteString(ch.Delta.Content.Text)
				for _, ann := range ch.Delta.Annotations {
					m, ok := ann.(map[string]interface{})
					if !ok {
						continue
					}
					if m["type"] == PrismToolEventAnnotationType {
						// payload kind tells us call vs result
						b, _ := m["payload"].([]byte)
						if b == nil {
							// json.RawMessage path
							if rm, ok := m["payload"].(interface{ MarshalJSON() ([]byte, error) }); ok {
								b, _ = rm.MarshalJSON()
							}
						}
						s := string(b)
						if strings.Contains(s, `"kind":"tool_call"`) {
							sawToolCallEvent = true
						}
						if strings.Contains(s, `"kind":"tool_result"`) {
							sawToolResultEv = true
						}
					}
				}
			}
		}
	}

	if got, want := text.String(), "It is now."; got != want {
		t.Errorf("aggregated content: got %q want %q", got, want)
	}
	if !sawStopFinish {
		t.Errorf("expected to see final stop finish chunk; got %d chunks", chunkCount)
	}
	if sawSuspectFinish {
		t.Errorf("trailing tool_calls finish chunk leaked to client")
	}
	if !sawFinalUsage {
		t.Errorf("expected final usage chunk to be forwarded")
	}
	if !sawToolCallEvent {
		t.Errorf("expected synthetic prism.tool_event of kind=tool_call")
	}
	if !sawToolResultEv {
		t.Errorf("expected synthetic prism.tool_event of kind=tool_result")
	}
	if got, want := mock.calls.Load(), int32(2); got != want {
		t.Errorf("expected provider Stream called %d times, got %d", want, got)
	}
}

// TestPipeline_StreamPassthrough_NoExtensions verifies that when no
// extensions/plugins are bound, the pipeline forwards every chunk verbatim
// (no tail suppression, no synthetic events).
func TestPipeline_StreamPassthrough_NoExtensions(t *testing.T) {
	mock := &streamingMockProvider{
		caps: llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat},
		streams: [][]api.StreamResult{
			{
				{Response: &api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{Content: api.Content{Text: "hi"}}}}}},
				{Response: &api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{}, FinishReason: "stop"}}}},
				{Response: &api.ChatResponse{Usage: &api.ResponseUsage{TotalTokens: 1}}},
			},
		},
	}
	orch := NewPipelineOrchestrator(plugin.NewRegistry(), extension.NewRegistry())
	stream, err := orch.Stream(context.Background(), &api.ChatRequest{
		Model:    "mock",
		Messages: []api.ChatMessage{{Role: "user", Content: api.Content{Text: "hi"}}},
	}, mock)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	count := 0
	for r := range stream {
		if r.Err != nil {
			t.Fatalf("stream err: %v", r.Err)
		}
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 forwarded chunks, got %d", count)
	}
}

// TestPipeline_Collect verifies the non-streaming Chat-style folder.
func TestPipeline_Collect(t *testing.T) {
	mock := &streamingMockProvider{
		caps: llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat},
		streams: [][]api.StreamResult{
			{
				{Response: &api.ChatResponse{ID: "gen-7", Model: "m", Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{Role: "assistant", Content: api.Content{Text: "hello "}}}}}},
				{Response: &api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{Content: api.Content{Text: "world"}}}}}},
				{Response: &api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{}, FinishReason: "stop"}}}},
				{Response: &api.ChatResponse{Usage: &api.ResponseUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}}},
			},
		},
	}
	orch := NewPipelineOrchestrator(plugin.NewRegistry(), extension.NewRegistry())
	stream, err := orch.Stream(context.Background(), &api.ChatRequest{
		Model:    "mock",
		Messages: []api.ChatMessage{{Role: "user", Content: api.Content{Text: "hi"}}},
	}, mock)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	resp, err := Collect(stream)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if resp.ID != "gen-7" {
		t.Errorf("id: %q", resp.ID)
	}
	if resp.Choices[0].Message.Content.Text != "hello world" {
		t.Errorf("content: %q", resp.Choices[0].Message.Content.Text)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish: %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 3 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}
