package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/adapters/providers/openai"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestOpenAIChat(t *testing.T) {
	// Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "gpt-3.5-turbo-0613",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello there!"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 9,
				"completion_tokens": 12,
				"total_tokens": 21
			}
		}`))
	}))
	defer server.Close()

	// Init Adapter
	adapter, err := openai.NewAdapter(domain.ProviderConfig{
		ID:      "openai-test",
		Type:    "openai",
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	assert.NoError(t, err)

	// Execute
	resp, err := adapter.Chat(context.Background(), &schema.ChatRequest{
		Model: "gpt-3.5-turbo",
		Messages: []schema.ChatMessage{
			{Role: "user", Content: schema.Content{Text: "Hi"}},
		},
	})

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Hello there!", resp.Choices[0].Message.Content.Text)
	assert.Equal(t, "openai-test", adapter.Name())
}
