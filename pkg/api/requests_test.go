package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatRequest_ToUpstream_StripsRouterOnlyFields(t *testing.T) {
	req := &ChatRequest{
		Model: "provider/model",
		Messages: []ChatMessage{
			{Role: "user", Content: Content{Text: "hello"}},
		},
		Plugins: []PluginConfig{
			{ID: "context-compression"},
		},
		Extensions: []ExtensionConfig{
			{ID: "prism:datetime"},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: FunctionDescription{
					Name: "get_weather",
				},
			},
		},
		Debug: &DebugOptions{EchoUpstreamBody: true},
		Route: "fallback",
		User:  "user-123",
	}

	upstream := req.ToUpstream()
	data, err := json.Marshal(upstream)
	if err != nil {
		t.Fatalf("marshal upstream request: %v", err)
	}

	payload := string(data)
	if strings.Contains(payload, `"plugins":`) || strings.Contains(payload, `"extensions":`) || strings.Contains(payload, `"debug":`) || strings.Contains(payload, `"route":`) || strings.Contains(payload, `"user":`) {
		t.Fatalf("upstream request leaked router-only fields: %s", payload)
	}
	if !strings.Contains(payload, `"tools"`) || !strings.Contains(payload, `"messages"`) || !strings.Contains(payload, `"model"`) {
		t.Fatalf("upstream request missing expected provider fields: %s", payload)
	}
}
