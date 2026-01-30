package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm/openai"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestOpenAIChat(t *testing.T) {
	// Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
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

		if err != nil {
			return
		}
	}))
	defer server.Close()

	// Init Adapter
	adapter, err := openai.NewAdapter(config.ProviderConfig{
		ID:      "openai-test",
		Type:    "openai",
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	assert.NoError(t, err)

	// Execute
	resp, err := adapter.Chat(context.Background(), &api.ChatRequest{
		Model: "gpt-3.5-turbo",
		Messages: []api.ChatMessage{
			{Role: "user", Content: api.Content{Text: "Hi"}},
		},
	})

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Hello there!", resp.Choices[0].Message.Content.Text)
	assert.Equal(t, "openai-test", adapter.Name())
}
