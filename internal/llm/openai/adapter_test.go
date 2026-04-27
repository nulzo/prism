package openai_test

import (
	"context"
	"encoding/json"
	"io"
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
	resp, err := adapter.Chat(context.Background(), &api.UpstreamChatRequest{
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

func TestOpenAIReasoningPayloadUsesProviderNativeShape(t *testing.T) {
	tests := []struct {
		name                 string
		baseURLPath          string
		wantReasoningEffort  bool
		wantStructuredReason bool
	}{
		{
			name:                "openai upstream uses reasoning_effort",
			baseURLPath:         "/v1",
			wantReasoningEffort: true,
		},
		{
			name:                 "openrouter upstream uses structured reasoning only",
			baseURLPath:          "/openrouter/v1",
			wantStructuredReason: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"id": "chatcmpl-123",
					"object": "chat.completion",
					"created": 1677652288,
					"model": "gpt-5",
					"choices": [{
						"index": 0,
						"message": {"role": "assistant", "content": "ok"},
						"finish_reason": "stop"
					}]
				}`))
			}))
			defer server.Close()

			adapter, err := openai.NewAdapter(config.ProviderConfig{
				ID:      "openai-test",
				Type:    "openai",
				APIKey:  "test-key",
				BaseURL: server.URL + tt.baseURLPath,
			})
			assert.NoError(t, err)

			enabled := true
			_, err = adapter.Chat(context.Background(), &api.UpstreamChatRequest{
				Model: "gpt-5",
				Messages: []api.ChatMessage{
					{Role: "user", Content: api.Content{Text: "Hi"}},
				},
				Reasoning: &api.ReasoningConfig{Enabled: &enabled, Effort: "high"},
			})
			assert.NoError(t, err)

			_, hasReasoningEffort := got["reasoning_effort"]
			_, hasStructuredReasoning := got["reasoning"]
			assert.Equal(t, tt.wantReasoningEffort, hasReasoningEffort)
			assert.Equal(t, tt.wantStructuredReason, hasStructuredReasoning)
		})
	}
}

func TestOpenAIImageModelUsesImagesGenerationsEndpoint(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/images/generations", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"created": 1677652288,
			"data": [{
				"b64_json": "aW1hZ2UtYnl0ZXM=",
				"revised_prompt": "A polished prompt"
			}]
		}`))
	}))
	defer server.Close()

	adapter, err := openai.NewAdapter(config.ProviderConfig{
		ID:      "openai-test",
		Type:    "openai",
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	assert.NoError(t, err)

	resp, err := adapter.Chat(context.Background(), &api.UpstreamChatRequest{
		Model: "gpt-image-2",
		Messages: []api.ChatMessage{
			{Role: "user", Content: api.Content{Text: "Draw an otter"}},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, "gpt-image-2", got["model"])
	assert.Equal(t, "Draw an otter", got["prompt"])
	assert.Equal(t, "A polished prompt", resp.Choices[0].Message.Content.Text)
	if assert.Len(t, resp.Choices[0].Message.Images, 1) {
		assert.Equal(t, "data:image/png;base64,aW1hZ2UtYnl0ZXM=", resp.Choices[0].Message.Images[0].ImageURL.URL)
	}
}

func TestOpenAIImageModelWithReferenceUsesImagesEditsEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/images/edits", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart body: %v", err)
		}
		assert.Equal(t, "gpt-image-2", r.FormValue("model"))
		assert.Equal(t, "Use this reference", r.FormValue("prompt"))

		files := r.MultipartForm.File["image[]"]
		if assert.Len(t, files, 1) {
			f, err := files[0].Open()
			if err != nil {
				t.Fatalf("open uploaded image: %v", err)
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				t.Fatalf("read uploaded image: %v", err)
			}
			assert.Equal(t, []byte("reference-bytes"), data)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"created": 1677652288,
			"data": [{"b64_json": "aW1hZ2UtYnl0ZXM="}]
		}`))
	}))
	defer server.Close()

	adapter, err := openai.NewAdapter(config.ProviderConfig{
		ID:      "openai-test",
		Type:    "openai",
		APIKey:  "test-key",
		BaseURL: server.URL + "/v1",
	})
	assert.NoError(t, err)

	resp, err := adapter.Chat(context.Background(), &api.UpstreamChatRequest{
		Model: "gpt-image-2",
		Messages: []api.ChatMessage{
			{
				Role: "user",
				Content: api.Content{Parts: []api.ContentPart{
					{Type: "text", Text: "Use this reference"},
					{Type: "image_url", ImageURL: &api.ImageURL{URL: "data:image/png;base64,cmVmZXJlbmNlLWJ5dGVz"}},
				}},
			},
		},
	})
	assert.NoError(t, err)
	if assert.Len(t, resp.Choices[0].Message.Images, 1) {
		assert.Equal(t, "data:image/png;base64,aW1hZ2UtYnl0ZXM=", resp.Choices[0].Message.Images[0].ImageURL.URL)
	}
}
