package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/processing"
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register("anthropic", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
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

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []Content
}
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	System    string    `json:"system,omitempty"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream,omitempty"`
}
type Response struct {
	ID         string    `json:"id"`
	Content    []Content `json:"content"`
	Model      string    `json:"model"`
	StopReason string    `json:"stop_reason"`
	Usage      Usage     `json:"usage"`
}
type Content struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *ImageSource `json:"source,omitempty"`
}
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg"
	Data      string `json:"data"`
}
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
type StreamEvent struct {
	Type         string   `json:"type"`
	Delta        *Delta   `json:"delta,omitempty"`
	ContentBlock *Content `json:"content_block,omitempty"` // For content_block_start
	Index        int      `json:"index,omitempty"`
	Usage        *Usage   `json:"usage,omitempty"` // For message_start
}
type Delta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Convert Unified -> Anthropic
func toAnthropicReq(req *api.ChatRequest) Request {
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
			var contentParts []Content

			// Handle simple string content
			if m.Content.Text != "" && len(m.Content.Parts) == 0 {
				contentParts = append(contentParts, Content{
					Type: "text",
					Text: m.Content.Text,
				})
			}

			// Handle multipart content
			for _, part := range m.Content.Parts {
				if part.Type == "text" {
					contentParts = append(contentParts, Content{
						Type: "text",
						Text: part.Text,
					})
				} else if part.Type == "image_url" && part.ImageURL != nil {
					imgData, err := processing.ProcessImageURL(part.ImageURL.URL)
					if err == nil {
						contentParts = append(contentParts, Content{
							Type: "image",
							Source: &ImageSource{
								Type:      "base64",
								MediaType: imgData.MediaType,
								Data:      imgData.Data,
							},
						})
					}
				}
			}

			if len(contentParts) > 0 {
				ar.Messages = append(ar.Messages, Message{
					Role:    m.Role,
					Content: contentParts,
				})
			}
		}
	}
	return ar
}

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	ar := toAnthropicReq(req)
	ar.Stream = false

	var anthroResp Response
	headers := map[string]string{
		"x-server-key":      a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if v, ok := a.config.Config["version"]; ok {
		headers["anthropic-version"] = v
	}

	url := fmt.Sprintf("%s/messages", strings.TrimRight(a.config.BaseURL, "/"))
	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, ar, &anthroResp); err != nil {
		return nil, err
	}

	// Convert Anthropic -> Unified
	fullText := ""
	for _, c := range anthroResp.Content {
		if c.Type == "text" {
			fullText += c.Text
		}
	}

	content, reasoning := processing.ExtractThinking(fullText)

	return &api.ChatResponse{
		ID:      anthroResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   anthroResp.Model,
		Choices: []api.Choice{{
			Index: 0,
			Message: &api.ChatMessage{
				Role:      "assistant",
				Content:   api.Content{Text: content},
				Reasoning: reasoning,
			},
			FinishReason: anthroResp.StopReason,
		}},
		Usage: &api.ResponseUsage{
			PromptTokens:     anthroResp.Usage.InputTokens,
			CompletionTokens: anthroResp.Usage.OutputTokens,
			TotalTokens:      anthroResp.Usage.InputTokens + anthroResp.Usage.OutputTokens,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)
	ar := toAnthropicReq(req)
	ar.Stream = true

	url := fmt.Sprintf("%s/messages", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"x-server-key":      a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if v, ok := a.config.Config["version"]; ok {
		headers["anthropic-version"] = v
	}

	go func() {
		defer close(ch)

		parser := processing.NewStreamParser()

		err := httpclient.StreamRequest(ctx, a.client, "POST", url, headers, ar, func(line string) error {
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
			case "message_start":
				if event.Usage != nil {
					// Input tokens are sent here
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Usage: &api.ResponseUsage{
							PromptTokens: event.Usage.InputTokens,
						},
					}}
				}
			case "content_block_delta":
				if event.Delta != nil && event.Delta.Type == "text_delta" {
					c, r := parser.Process(event.Delta.Text)
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Choices: []api.Choice{{
							Delta: &api.ChatMessage{
								Content:   api.Content{Text: c},
								Reasoning: r,
							},
						}},
					}}
				}
			case "message_delta":
				// Output tokens and stop reason sent here
				if event.Usage != nil {
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Usage: &api.ResponseUsage{
							CompletionTokens: event.Usage.OutputTokens,
						},
					}}
				}
				// if event.Delta != nil && event.Delta.Type == "stop_reason" {
				// 	// stop reason logic handled in message_stop usually, but sometimes here?
				// 	// Anthropic docs say stop_reason is in message_delta
				// }
			case "message_stop":
				ch <- api.StreamResult{Response: &api.ChatResponse{
					Choices: []api.Choice{{
						FinishReason: "stop",
						Delta:        &api.ChatMessage{},
					}},
				}}
			}
			return nil
		})

		if err != nil {
			ch <- api.StreamResult{Err: err}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	// Anthropic provider uses static configuration
	return a.config.StaticModels, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	// Anthropic's "list models" endpoint is a good candidate for a health check
	// as it requires auth and verifies connectivity.
	url := "https://api.anthropic.com/v1/models?limit=1"
	if a.config.BaseURL != "" {
		url = fmt.Sprintf("%s/v1/models?limit=1", strings.TrimRight(a.config.BaseURL, "/"))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("x-api-key", a.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	if v, ok := a.config.Config["version"]; ok {
		req.Header.Set("anthropic-version", v)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	return nil
}
