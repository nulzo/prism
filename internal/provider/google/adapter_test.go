package google_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/internal/provider/google"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestChat_RealCallStructure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "generateContent")
		assert.Equal(t, "POST", r.Method)
		
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		
		contents := body["contents"].([]interface{})
		assert.NotEmpty(t, contents)

		// Respond
		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"parts": []map[string]interface{}{
							{"text": "Gemini response"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]int{
				"totalTokenCount": 100,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := provider.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
	}
	adapter := google.NewAdapter(cfg)

	req := &schema.ChatRequest{
		Model: "gemini-pro",
		Messages: []schema.ChatMessage{
			{Role: "user", Content: schema.Content{Text: "Explain Go"}},
		},
	}

	resp, err := adapter.Chat(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, "Gemini response", resp.Choices[0].Message.Content.Text)
}
