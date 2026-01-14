package openai_test

import (
	"context"
	"testing"

	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/internal/provider/openai"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
)

// Since the current adapter is mocking the response internally (mock mode),
// we test that the mock returns expected structure.
// When real HTTP calls are implemented, we will use httptest.NewServer.

func TestChat_Mock(t *testing.T) {
	cfg := provider.ProviderConfig{
		APIKey: "sk-mock-openai",
	}
	adapter := openai.NewAdapter(cfg)

	req := &schema.ChatRequest{
		Model:    "gpt-4",
		Messages: []schema.ChatMessage{{Role: "user", Content: schema.Content{Text: "Hello"}}},
	}

	resp, err := adapter.Chat(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "gpt-4", resp.Model)
	assert.Contains(t, resp.Choices[0].Message.Content.Text, "Echoing: Hello")
}

func TestStream_Mock(t *testing.T) {
	cfg := provider.ProviderConfig{
		APIKey: "sk-mock-openai",
	}
	adapter := openai.NewAdapter(cfg)

	req := &schema.ChatRequest{
		Model:    "gpt-4",
		Messages: []schema.ChatMessage{{Role: "user", Content: schema.Content{Text: "StreamMe"}}},
		Stream:   true,
	}

	ch, err := adapter.Stream(context.Background(), req)
	assert.NoError(t, err)

	var fullContent string
	for res := range ch {
		assert.NoError(t, res.Err)
		if res.Response != nil && len(res.Response.Choices) > 0 {
			if res.Response.Choices[0].Delta != nil {
				fullContent += res.Response.Choices[0].Delta.Content.Text
			}
		}
	}

	assert.Contains(t, fullContent, "Echoing: StreamMe")
}
