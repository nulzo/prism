package gateway

import (
	"testing"

	"github.com/nulzo/model-router-api/pkg/api"
)

func TestToolCallAccumulator_PositionalMerge(t *testing.T) {
	acc := NewToolCallAccumulator()

	// Chunk 1: introduces slot 0 with id+name and start of arguments
	acc.Apply([]api.ToolCall{
		{ID: "call_1", Type: "function", Function: api.FunctionCall{Name: "get_weather", Arguments: `{"loc`}},
	})
	// Chunk 2: continuation — id/name omitted, arguments appended
	acc.Apply([]api.ToolCall{
		{Function: api.FunctionCall{Arguments: `ation":"SF"}`}},
	})

	calls := acc.Snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ID != "call_1" {
		t.Fatalf("id not preserved: %q", calls[0].ID)
	}
	if calls[0].Function.Name != "get_weather" {
		t.Fatalf("name not preserved: %q", calls[0].Function.Name)
	}
	want := `{"location":"SF"}`
	if calls[0].Function.Arguments != want {
		t.Fatalf("arguments mismatch: got %q want %q", calls[0].Function.Arguments, want)
	}
}

func TestToolCallAccumulator_MultipleSlots(t *testing.T) {
	acc := NewToolCallAccumulator()
	acc.ApplyAt(0, api.ToolCall{ID: "a", Function: api.FunctionCall{Name: "x", Arguments: `{"a":1}`}})
	acc.ApplyAt(2, api.ToolCall{ID: "c", Function: api.FunctionCall{Name: "z", Arguments: `{"c":3}`}})
	acc.ApplyAt(1, api.ToolCall{ID: "b", Function: api.FunctionCall{Name: "y", Arguments: `{"b":2}`}})

	calls := acc.Snapshot()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if calls[0].ID != "a" || calls[1].ID != "b" || calls[2].ID != "c" {
		t.Fatalf("slot order broken: %+v", calls)
	}
}

func TestStreamAggregator_FoldsTextReasoningToolCalls(t *testing.T) {
	agg := NewStreamAggregator()

	apply := func(chunk *api.ChatResponse) { agg.Apply(chunk) }

	apply(&api.ChatResponse{ID: "gen-123", Model: "openai/gpt-4o", Choices: []api.Choice{{
		Index: 0, Delta: &api.ChatMessage{Role: "assistant"},
	}}})
	apply(&api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{
		Reasoning: "Thinking...", Content: api.Content{Text: "Hello"},
	}}}})
	apply(&api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{
		Content: api.Content{Text: " world"}, ToolCalls: []api.ToolCall{
			{ID: "call_x", Type: "function", Function: api.FunctionCall{Name: "search", Arguments: `{"q":"go"}`}},
		},
	}}}})
	apply(&api.ChatResponse{Choices: []api.Choice{{Index: 0, Delta: &api.ChatMessage{}, FinishReason: "tool_calls"}}})
	apply(&api.ChatResponse{Usage: &api.ResponseUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}})

	if got := agg.FinishReason(); got != "tool_calls" {
		t.Fatalf("finish_reason: got %q want tool_calls", got)
	}
	resp := agg.FinalResponse()
	if resp.ID != "gen-123" {
		t.Fatalf("id: got %q want gen-123", resp.ID)
	}
	if resp.Choices[0].Message.Content.Text != "Hello world" {
		t.Fatalf("content: got %q", resp.Choices[0].Message.Content.Text)
	}
	if resp.Choices[0].Message.Reasoning != "Thinking..." {
		t.Fatalf("reasoning: got %q", resp.Choices[0].Message.Reasoning)
	}
	if calls := resp.Choices[0].Message.ToolCalls; len(calls) != 1 || calls[0].Function.Name != "search" {
		t.Fatalf("tool_calls: got %+v", calls)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 10 {
		t.Fatalf("usage not preserved: %+v", resp.Usage)
	}
}

func TestStreamAggregator_NilChunkSafe(t *testing.T) {
	agg := NewStreamAggregator()
	agg.Apply(nil) // must not panic
	resp := agg.FinalResponse()
	if resp == nil {
		t.Fatalf("FinalResponse returned nil")
	}
}

func TestToolCallAccumulator_NoIndexWithIDFallback(t *testing.T) {
	acc := NewToolCallAccumulator()

	// Chunk 1: introduces call_1 with ID but no Index
	acc.Apply([]api.ToolCall{
		{ID: "call_1", Type: "function", Function: api.FunctionCall{Name: "get_weather", Arguments: `{"loc`}},
	})
	
	// Chunk 2: continuation for call_1, ID matches, no Index
	acc.Apply([]api.ToolCall{
		{ID: "call_1", Function: api.FunctionCall{Arguments: `ation":"SF"}`}},
	})
	
	// Chunk 3: introduces call_2 with ID but no Index
	acc.Apply([]api.ToolCall{
		{ID: "call_2", Type: "function", Function: api.FunctionCall{Name: "get_time", Arguments: `{"tz":"`}},
	})
	
	// Chunk 4: continuation for call_2 with no ID and no Index (appends to active maxIdx)
	acc.Apply([]api.ToolCall{
		{Function: api.FunctionCall{Arguments: `UTC"}`}},
	})

	calls := acc.Snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	
	if calls[0].ID != "call_1" || calls[0].Function.Name != "get_weather" || calls[0].Function.Arguments != `{"location":"SF"}` {
		t.Fatalf("call_1 mismatch: %+v", calls[0])
	}
	
	if calls[1].ID != "call_2" || calls[1].Function.Name != "get_time" || calls[1].Function.Arguments != `{"tz":"UTC"}` {
		t.Fatalf("call_2 mismatch: %+v", calls[1])
	}
}
