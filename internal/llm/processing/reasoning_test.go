package processing

import (
	"encoding/json"
	"testing"

	"github.com/nulzo/model-router-api/pkg/api"
)

func TestExtractThinking(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantContent   string
		wantReasoning string
	}{
		{
			name:          "No thinking",
			input:         "Hello world",
			wantContent:   "Hello world",
			wantReasoning: "",
		},
		{
			name:          "Simple thinking",
			input:         "<think>Reasoning here</think>Hello world",
			wantContent:   "Hello world",
			wantReasoning: "Reasoning here",
		},
		{
			name:          "Thinking at end",
			input:         "Hello world<think>Reasoning here</think>",
			wantContent:   "Hello world",
			wantReasoning: "Reasoning here",
		},
		{
			name:          "Thinking in middle",
			input:         "Hello <think>Reasoning</think> world",
			wantContent:   "Hello  world",
			wantReasoning: "Reasoning",
		},
		{
			name:          "Multiple thinking blocks",
			input:         "<think>R1</think>C1<think>R2</think>C2",
			wantContent:   "C1C2",
			wantReasoning: "R1R2",
		},
		{
			name:          "Unclosed thinking",
			input:         "Hello <think>Reasoning",
			wantContent:   "Hello ",
			wantReasoning: "Reasoning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotContent, gotReasoning := ExtractThinking(tt.input)
			if gotContent != tt.wantContent {
				t.Errorf("ExtractThinking() gotContent = %v, want %v", gotContent, tt.wantContent)
			}
			if gotReasoning != tt.wantReasoning {
				t.Errorf("ExtractThinking() gotReasoning = %v, want %v", gotReasoning, tt.wantReasoning)
			}
		})
	}
}

func TestStreamParser(t *testing.T) {
	tests := []struct {
		name          string
		chunks        []string
		wantContent   string
		wantReasoning string
	}{
		{
			name:          "Simple split",
			chunks:        []string{"<thi", "nk>Reasoning</th", "ink>Hello"},
			wantContent:   "Hello",
			wantReasoning: "Reasoning",
		},
		{
			name:          "Tag split byte by byte",
			chunks:        []string{"<", "t", "h", "i", "n", "k", ">", "R", "<", "/", "t", "h", "i", "n", "k", ">", "C"},
			wantContent:   "C",
			wantReasoning: "R",
		},
		{
			name:          "Partial at end of stream",
			chunks:        []string{"Hello <thi"},
			wantContent:   "Hello <thi", // Should be flushed as content if stream ends
			wantReasoning: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewStreamParser()
			fullContent := ""
			fullReasoning := ""

			for _, chunk := range tt.chunks {
				c, r := p.Process(chunk)
				fullContent += c
				fullReasoning += r
			}

			if tt.name == "Partial at end of stream" {
				// Special check
				if fullContent != "Hello " {
					t.Errorf("StreamParser final content = %q, want %q (ignoring buffer)", fullContent, "Hello ")
				}
				return
			}

			if fullContent != tt.wantContent {
				t.Errorf("StreamParser content = %q, want %q", fullContent, tt.wantContent)
			}
			if fullReasoning != tt.wantReasoning {
				t.Errorf("StreamParser reasoning = %q, want %q", fullReasoning, tt.wantReasoning)
			}
		})
	}
}

func TestCompatMessageReasoningContent(t *testing.T) {
	msg := api.ChatMessage{
		Role:      "assistant",
		Reasoning: "native thinking",
		Content:   api.Content{Text: "calling a tool"},
		ToolCalls: []api.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: api.FunctionCall{
				Name:      "search",
				Arguments: "{}",
			},
		}},
	}

	raw, err := json.Marshal(CompatMessage{Message: msg})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["reasoning"]; ok {
		t.Fatalf("expected reasoning to be stripped, got %s", raw)
	}
	if got["reasoning_content"] != "native thinking" {
		t.Fatalf("reasoning_content = %v, want native thinking; raw=%s", got["reasoning_content"], raw)
	}
}

func TestCompatMessageMissingToolCallReasoningUsesNonEmptyPlaceholder(t *testing.T) {
	msg := api.ChatMessage{
		Role:    "assistant",
		Content: api.Content{Text: "calling a tool"},
		ToolCalls: []api.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: api.FunctionCall{
				Name:      "search",
				Arguments: "{}",
			},
		}},
	}

	raw, err := json.Marshal(CompatMessage{Message: msg})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["reasoning_content"] != " " {
		t.Fatalf("reasoning_content = %q, want single-space placeholder; raw=%s", got["reasoning_content"], raw)
	}
}
