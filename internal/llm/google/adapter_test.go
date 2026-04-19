package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestShape_ReferenceImage(t *testing.T) {
	// 1. Create a request with text + reference image (base64)
	req := &api.UpstreamChatRequest{
		Model: "gemini-2.5-flash-image",
		Messages: []api.ChatMessage{
			{
				Role: "user",
				Content: api.Content{
					Parts: []api.ContentPart{
						{
							Type: "text",
							Text: "Turn this sketch into a realistic image.",
						},
						{
							Type: "image_url",
							ImageURL: &api.ImageURL{
								// Simple 1x1 red pixel png
								URL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
							},
						},
					},
				},
			},
		},
		Modalities:  []string{"image", "text"},
		Temperature: 0.7,
	}

	// 2. Call Shape
	geminiReq, err := Shape(req)
	assert.NoError(t, err)

	// 3. Verify Generation Config (Modalities)
	assert.NotNil(t, geminiReq.GenerationConfig)
	assert.Contains(t, geminiReq.GenerationConfig.ResponseModalities, "IMAGE")
	assert.Contains(t, geminiReq.GenerationConfig.ResponseModalities, "TEXT")
	assert.Equal(t, 0.7, geminiReq.GenerationConfig.Temperature)

	// 4. Verify Content (Text + Image)
	assert.Len(t, geminiReq.Contents, 1)
	content := geminiReq.Contents[0]
	assert.Equal(t, "user", content.Role)
	assert.Len(t, content.Parts, 2)

	// Part 1: Text
	assert.Equal(t, "Turn this sketch into a realistic image.", content.Parts[0].Text)

	// Part 2: Image
	assert.Empty(t, content.Parts[1].Text) // Should be empty for image part
	assert.NotNil(t, content.Parts[1].InlineData)
	assert.Equal(t, "image/png", content.Parts[1].InlineData.MimeType)
	assert.NotEmpty(t, content.Parts[1].InlineData.Data)
}

func TestShape_SimpleText(t *testing.T) {
	req := &api.UpstreamChatRequest{
		Model: "gemini-pro",
		Messages: []api.ChatMessage{
			{
				Role: "user",
				Content: api.Content{
					Text: "Hello!",
				},
			},
		},
	}

	geminiReq, err := Shape(req)
	assert.NoError(t, err)

	assert.Len(t, geminiReq.Contents, 1)
	assert.Equal(t, "user", geminiReq.Contents[0].Role)
	assert.Equal(t, "Hello!", geminiReq.Contents[0].Parts[0].Text)
	// No generation config if not specified
	assert.Nil(t, geminiReq.GenerationConfig)
}

func TestChat_UsesOpenAICompatWhenToolsPresent(t *testing.T) {
	var gotPath string
	var gotTools int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		var req api.UpstreamChatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		gotTools = len(req.Tools)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"choices":[
				{
					"index":0,
					"message":{
						"role":"assistant",
						"tool_calls":[
							{
								"id":"call_1",
								"type":"function",
									"extra_content":{
										"google":{
											"thought_signature":"sig-123"
										}
									},
								"function":{
									"name":"prism:datetime",
									"arguments":"{\"timezone\":\"Asia/Tokyo\"}"
								}
							}
						]
					},
					"finish_reason":"tool_calls"
				}
			],
			"created":123,
			"model":"gemini-3.1-flash-lite-preview",
			"object":"chat.completion",
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`))
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.ProviderConfig{
		ID:      "google-test",
		Type:    "google",
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	assert.NoError(t, err)

	req := &api.UpstreamChatRequest{
		Model: "gemini-3.1-flash-lite-preview",
		Messages: []api.ChatMessage{
			{Role: "user", Content: api.Content{Text: "What time is it in Tokyo?"}},
		},
		Tools: []api.Tool{
			{
				Type: "function",
				Function: api.FunctionDescription{
					Name:        "prism:datetime",
					Description: "Get the current date and time.",
					Parameters: map[string]interface{}{
						"type": "object",
					},
				},
			},
		},
	}

	resp, err := adapter.Chat(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, "/openai/chat/completions", gotPath)
	assert.Equal(t, 1, gotTools)
	if assert.Len(t, resp.Choices, 1) {
		assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
		if assert.NotNil(t, resp.Choices[0].Message) && assert.Len(t, resp.Choices[0].Message.ToolCalls, 1) {
			assert.Equal(t, "prism:datetime", resp.Choices[0].Message.ToolCalls[0].Function.Name)
			if assert.NotNil(t, resp.Choices[0].Message.ToolCalls[0].ExtraContent) && assert.NotNil(t, resp.Choices[0].Message.ToolCalls[0].ExtraContent.Google) {
				assert.Equal(t, "sig-123", resp.Choices[0].Message.ToolCalls[0].ExtraContent.Google.ThoughtSignature)
			}
		}
	}
}
