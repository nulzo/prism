package openai

import (
	"context"
	"fmt"
	"time"

	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Adapter struct {
	config provider.ProviderConfig
}

func NewAdapter(config provider.ProviderConfig) *Adapter {
	return &Adapter{config: config}
}

func (a *Adapter) Name() string {
	return "openai"
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	// Simple mock chat
	return &schema.ChatResponse{
		ID:      "chatcmpl-mock-openai-" + fmt.Sprintf("%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: &schema.ChatMessage{
					Role:    "assistant",
					Content: schema.Content{Text: fmt.Sprintf("[OpenAI] Echoing: %s", lastUserContent(req))},
				},
				FinishReason: "stop",
			},
		},
		Usage: &schema.ResponseUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan provider.StreamResult, error) {
	ch := make(chan provider.StreamResult)

	go func() {
		defer close(ch)

		content := fmt.Sprintf("[OpenAI Streaming] Echoing: %s", lastUserContent(req))
		tokens := []rune(content)
		chunkSize := 5

		for i := 0; i < len(tokens); i += chunkSize {
			end := i + chunkSize
			if end > len(tokens) {
				end = len(tokens)
			}
			chunk := string(tokens[i:end])

			// Simulate network delay
			select {
			case <-ctx.Done():
				ch <- provider.StreamResult{Err: ctx.Err()}
				return
			case <-time.After(100 * time.Millisecond):
			}

			ch <- provider.StreamResult{
				Response: &schema.ChatResponse{
					ID:      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", time.Now().Unix()),
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   req.Model,
					Choices: []schema.Choice{
						{
							Index: 0,
							Delta: &schema.ChatMessage{
								Content: schema.Content{Text: chunk},
							},
						},
					},
				},
			}
		}

		// Final chunk with finish reason (optional in some implementations, but good practice)
		ch <- provider.StreamResult{
			Response: &schema.ChatResponse{
				ID:      "chatcmpl-mock-stream-" + fmt.Sprintf("%d", time.Now().Unix()),
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []schema.Choice{
					{
						Index:        0,
						Delta:        &schema.ChatMessage{}, // Empty delta
						FinishReason: "stop",
					},
				},
			},
		}
	}()

	return ch, nil
}

func lastUserContent(req *schema.ChatRequest) string {
	if len(req.Messages) > 0 {
		return req.Messages[len(req.Messages)-1].Content.Text
	}
	return ""
}
