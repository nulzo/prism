package anthropic

import (
	"bytes"
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
		config.BaseURL = "https://api.anthropic.com/v1"
	}
	if config.Version == "" {
		config.Version = "2023-06-01"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Adapter) Name() string {
	return "anthropic"
}

// Anthropic specific structures
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []AnthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
}

type AnthropicResponseContent struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
}

type AnthropicResponse struct {
	ID           string                     `json:"id"`
	Type         string                     `json:"type"`
	Role         string                     `json:"role"`
	Content      []AnthropicResponseContent `json:"content"`
	Model        string                     `json:"model"`
	StopReason   string                     `json:"stop_reason"`
	StopSequence string                     `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	// 1. Transformation (Unified -> Anthropic)
	anthropicReq := AnthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    false,
	}
	if anthropicReq.MaxTokens == 0 {
		anthropicReq.MaxTokens = 4096 // Default safe limit
	}

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			anthropicReq.System += msg.Content.Text + "\n"
		} else {
			anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
				Role:    msg.Role,
				Content: msg.Content.Text,
			})
		}
	}

	// 2. Execution (Real HTTP Request logic)
	// If API Key is "mock", return mock response to avoid network errors in demo
	if a.config.APIKey == "sk-mock-anthropic" {
		return a.mockResponse(req, anthropicReq)
	}

	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.config.BaseURL+"/messages", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("x-api-key", a.config.APIKey)
	httpReq.Header.Set("anthropic-version", a.config.Version)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic api call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic api error %d: %s", resp.StatusCode, string(body))
	}

	var anthroResp AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthroResp); err != nil {
		return nil, fmt.Errorf("failed to decode anthropic response: %w", err)
	}

	// 3. Transformation (Anthropic -> Unified)
	fullContent := ""
	for _, c := range anthroResp.Content {
		if c.Type == "text" {
			fullContent += c.Text
		}
	}

	return &schema.ChatResponse{
		ID:      anthroResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   anthroResp.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: &schema.ChatMessage{
					Role:    "assistant",
					Content: schema.Content{Text: fullContent},
				},
				FinishReason: anthroResp.StopReason,
			},
		},
		Usage: &schema.ResponseUsage{
			PromptTokens:     anthroResp.Usage.InputTokens,
			CompletionTokens: anthroResp.Usage.OutputTokens,
			TotalTokens:      anthroResp.Usage.InputTokens + anthroResp.Usage.OutputTokens,
		},
	}, nil
}

func (a *Adapter) mockResponse(req *schema.ChatRequest, anthroReq AnthropicRequest) (*schema.ChatResponse, error) {
	content := ""
	if len(req.Messages) > 0 {
		content = req.Messages[len(req.Messages)-1].Content.Text
	}
	return &schema.ChatResponse{
		ID:      "msg_mock_anthropic_" + fmt.Sprintf("%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: &schema.ChatMessage{
					Role:    "assistant",
					Content: schema.Content{Text: fmt.Sprintf("[Anthropic 2026] Echoing: %s (Model: %s)", content, req.Model)},
				},
				FinishReason: "stop",
			},
		},
		Usage: &schema.ResponseUsage{
			PromptTokens:     15,
			CompletionTokens: 25,
			TotalTokens:      40,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan provider.StreamResult, error) {
	ch := make(chan provider.StreamResult)

	go func() {
		defer close(ch)
		// Mock streaming implementation
		content := ""
		if len(req.Messages) > 0 {
			content = req.Messages[len(req.Messages)-1].Content.Text
		}
		
		responseContent := fmt.Sprintf("[Anthropic Stream] Echoing: %s", content)
		tokens := []rune(responseContent)
		chunkSize := 3

		for i := 0; i < len(tokens); i += chunkSize {
			end := i + chunkSize
			if end > len(tokens) {
				end = len(tokens)
			}
			
			select {
			case <-ctx.Done():
				ch <- provider.StreamResult{Err: ctx.Err()}
				return
			case <-time.After(50 * time.Millisecond):
			}

			ch <- provider.StreamResult{
				Response: &schema.ChatResponse{
					ID:      "msg_mock_stream_" + fmt.Sprintf("%d", time.Now().Unix()),
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   req.Model,
					Choices: []schema.Choice{
						{
							Index: 0,
							Delta: &schema.ChatMessage{
									Content: schema.Content{Text: string(tokens[i:end])},
							},
						},
					},
				},
			}
		}
		
		ch <- provider.StreamResult{
			Response: &schema.ChatResponse{
				Choices: []schema.Choice{{ 
					Index: 0,
					Delta: &schema.ChatMessage{},
					FinishReason: "stop",
				}}},
		}
	}()

	return ch, nil
}

type AnthropicModel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	CreatedAt   string `json:"created_at"`
	DisplayName string `json:"display_name"`
}

type AnthropicModelList struct {
	Data    []AnthropicModel `json:"data"`
	HasMore bool             `json:"has_more"`
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	// Helper to get static models
	getStaticModels := func() []schema.Model {
		var models []schema.Model
		for id, info := range modeldata.KnownModels {
			// Check if it looks like an Anthropic model ID
			if len(id) > 6 && id[:6] == "claude" {
				m := info
				m.ID = id
				m.Object = "model"
				m.Created = time.Now().Unix()
				m.OwnedBy = "anthropic"
				m.Provider = "anthropic"
				models = append(models, m)
			}
		}
		return models
	}

	if a.config.APIKey == "sk-mock-anthropic" || a.config.APIKey == "" {
		return getStaticModels(), nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", a.config.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.config.APIKey)
	req.Header.Set("anthropic-version", a.config.Version)

	resp, err := a.client.Do(req)
	if err != nil {
		return getStaticModels(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return getStaticModels(), nil
	}

	var list AnthropicModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	var models []schema.Model
	for _, m := range list.Data {
		t, _ := time.Parse(time.RFC3339, m.CreatedAt)
		
		mod := schema.Model{
			ID:       m.ID,
			Created:  t.Unix(),
			Object:   "model",
			OwnedBy:  "anthropic",
			Provider: "anthropic",
		}

		// Enrich
		if info, ok := modeldata.KnownModels[m.ID]; ok {
			mod.Name = info.Name
			mod.Description = info.Description
			mod.ContextLength = info.ContextLength
			mod.Architecture = info.Architecture
			mod.Pricing = info.Pricing
			mod.TopProvider = info.TopProvider
		} else {
			mod.Name = m.DisplayName
			if mod.Name == "" {
				mod.Name = m.ID
			}
			mod.Description = "Anthropic model"
			mod.Pricing = schema.Pricing{Prompt: "0", Completion: "0"}
		}
		
		models = append(models, mod)
	}

	return models, nil
}
