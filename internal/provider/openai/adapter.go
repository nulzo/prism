package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nulzo/model-router-api/internal/modeldata"
	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Adapter struct {
	config provider.ProviderConfig
	client *http.Client
}

func NewAdapter(config provider.ProviderConfig) *Adapter {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}
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

		// Final chunk with finish reason
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

type OpenAIModelList struct {
	Object string         `json:"object"`
	Data   []schema.Model `json:"data"`
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	if a.config.APIKey == "mock-openai-key" || a.config.APIKey == "" {
		// Return a few mock enriched models
		var models []schema.Model
		for id, info := range modeldata.KnownModels {
			if id == "gpt-4o" || id == "gpt-3.5-turbo" {
				m := info
				m.ID = id
				m.Object = "model"
				m.Created = time.Now().Unix()
				m.OwnedBy = "openai"
				m.Provider = "openai"
				models = append(models, m)
			}
		}
		return models, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", a.config.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai models api error %d: %s", resp.StatusCode, string(body))
	}

	var list OpenAIModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	var enrichedModels []schema.Model
	for _, m := range list.Data {
		m.Provider = "openai"
		
		// Enrich with known data
		if info, ok := modeldata.KnownModels[m.ID]; ok {
			m.Name = info.Name
			m.Description = info.Description
			m.ContextLength = info.ContextLength
			m.Architecture = info.Architecture
			m.Pricing = info.Pricing
			m.TopProvider = info.TopProvider
		} else {
			// Defaults for unknown models
			m.Name = m.ID
			m.Description = "OpenAI model"
			m.Pricing = schema.Pricing{Prompt: "0", Completion: "0"}
		}
		enrichedModels = append(enrichedModels, m)
	}

	return enrichedModels, nil
}

func lastUserContent(req *schema.ChatRequest) string {
	if len(req.Messages) > 0 {
		return req.Messages[len(req.Messages)-1].Content.Text
	}
	return ""
}