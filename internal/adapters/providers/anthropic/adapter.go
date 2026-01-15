package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/adapters/providers/utils"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/registry"
	"github.com/nulzo/model-router-api/pkg/schema"
)

func init() {
	registry.Register("anthropic", NewAdapter)
}

type Adapter struct {
	config domain.ProviderConfig
	client *http.Client
}

func NewAdapter(config domain.ProviderConfig) (ports.ModelProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.anthropic.com/v1"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return "anthropic" }

// --- Anthropic Internal Schemas ---
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	System    string    `json:"system,omitempty"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
}
type Response struct {
	ID           string    `json:"id"`
	Content      []Content `json:"content"`
	Model        string    `json:"model"`
	StopReason   string    `json:"stop_reason"`
	Usage        Usage     `json:"usage"`
}
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
type StreamEvent struct {
	Type         string    `json:"type"`
	Delta        *Delta    `json:"delta,omitempty"`
	ContentBlock *Content  `json:"content_block,omitempty"` // For content_block_start
	Index        int       `json:"index,omitempty"`
	Usage        *Usage    `json:"usage,omitempty"` // For message_start
}
type Delta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Convert Unified -> Anthropic
func toAnthropicReq(req *schema.ChatRequest) Request {
	ar := Request{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}
	if ar.MaxTokens == 0 {
		ar.MaxTokens = 4096
	}
	for _, m := range req.Messages {
		if m.Role == "system" {
			ar.System += m.Content.Text + "\n"
		} else {
			ar.Messages = append(ar.Messages, Message{
				Role:    m.Role,
				Content: m.Content.Text,
			})
		}
	}
	return ar
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	ar := toAnthropicReq(req)
	ar.Stream = false

	var anthroResp Response
	headers := map[string]string{
		"x-api-key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if v, ok := a.config.Config["version"]; ok {
		headers["anthropic-version"] = v
	}

	url := fmt.Sprintf("%s/messages", strings.TrimRight(a.config.BaseURL, "/"))
	if err := utils.SendRequest(ctx, a.client, "POST", url, headers, ar, &anthroResp); err != nil {
		return nil, err
	}

	// Convert Anthropic -> Unified
	fullText := ""
	for _, c := range anthroResp.Content {
		if c.Type == "text" {
			fullText += c.Text
		}
	}

	return &schema.ChatResponse{
		ID:      anthroResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   anthroResp.Model,
		Choices: []schema.Choice{{
			Index: 0,
			Message: &schema.ChatMessage{
				Role:    "assistant",
				Content: schema.Content{Text: fullText},
			},
			FinishReason: anthroResp.StopReason,
		}},
		Usage: &schema.ResponseUsage{
			PromptTokens:     anthroResp.Usage.InputTokens,
			CompletionTokens: anthroResp.Usage.OutputTokens,
			TotalTokens:      anthroResp.Usage.InputTokens + anthroResp.Usage.OutputTokens,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	ch := make(chan ports.StreamResult)
	ar := toAnthropicReq(req)
	ar.Stream = true

	url := fmt.Sprintf("%s/messages", strings.TrimRight(a.config.BaseURL, "/"))
	
	headers := map[string]string{
		"x-api-key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if v, ok := a.config.Config["version"]; ok {
		headers["anthropic-version"] = v
	}

	go func() {
		defer close(ch)

		err := utils.StreamRequest(ctx, a.client, "POST", url, headers, ar, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}
			data := strings.TrimPrefix(line, "data: ")
			
			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return nil
			}

			// Map Anthropic Events to OpenAI-compatible chunks
			switch event.Type {
			case "content_block_delta":
				if event.Delta != nil && event.Delta.Type == "text_delta" {
					ch <- ports.StreamResult{Response: &schema.ChatResponse{
						Choices: []schema.Choice{{
							Delta: &schema.ChatMessage{Content: schema.Content{Text: event.Delta.Text}},
						}},
					}}
				}
			case "message_stop":
				ch <- ports.StreamResult{Response: &schema.ChatResponse{
					Choices: []schema.Choice{{
						FinishReason: "stop",
						Delta:        &schema.ChatMessage{},
					}},
				}}
			}
			return nil
		})

		if err != nil {
			ch <- ports.StreamResult{Err: err}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	// Anthropic doesn't have a robust public models list endpoint that returns all metadata standardly like OpenAI
	// But we can try /v1/models if available, or return static knowns if 404.
	// For this implementation, we'll try to fetch but fallback gracefully.
	// NOTE: Anthropic recently added a models endpoint.
	
	var list struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	
	url := fmt.Sprintf("%s/models", strings.TrimRight(a.config.BaseURL, "/"))
	headers := map[string]string{
		"x-api-key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}

	if err := utils.SendRequest(ctx, a.client, "GET", url, headers, nil, &list); err != nil {
		// Fallback to static list if API fails (common for Anthropic which limits this endpoint)
		// Return empty list so we don't block the router's aggregation
		return []schema.Model{}, nil
	}

	var models []schema.Model
	for _, m := range list.Data {
		t, _ := time.Parse(time.RFC3339, m.CreatedAt)
		models = append(models, schema.Model{
			ID:       m.ID,
			Name:     m.DisplayName,
			Created:  t.Unix(),
			Provider: a.config.ID,
			OwnedBy:  "anthropic",
		})
	}
	return models, nil
}
