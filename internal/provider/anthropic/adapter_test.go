package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/internal/provider/anthropic"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestChat_RealCallStructure(t *testing.T) {
	// Mock 3rd party server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		
		// Verify system prompt logic
		assert.Equal(t, "claude-3", body["model"])
		assert.Equal(t, "You are helpful\n", body["system"]) // System prompt separated

		// Respond
		resp := map[string]interface{}{
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"model": "claude-3",
			"content": []map[string]interface{}{
				{"type": "text", "text": "Hello there"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]int{"input_tokens": 10, "output_tokens": 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := provider.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL, // Point to mock server
	}
	adapter := anthropic.NewAdapter(cfg)

	req := &schema.ChatRequest{
		Model: "claude-3",
		Messages: []schema.ChatMessage{
			{Role: "system", Content: schema.Content{Text: "You are helpful"}},
			{Role: "user", Content: schema.Content{Text: "Hi"}},
		},
	}

	resp, err := adapter.Chat(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, "Hello there", resp.Choices[0].Message.Content.Text)
}
