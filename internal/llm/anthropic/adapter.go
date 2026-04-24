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
	stream *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.anthropic.com/v1"
	}

	timeout := 10 * time.Minute
	if config.Timeout != "" {
		if d, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = d
		} else {
			fmt.Printf("Warning: Invalid timeout format for provider %s: %v. Using default %v.\n", config.ID, err, timeout)
		}
	}

	return &Adapter{
		config: config,
		client: httpclient.NewRequestClient(timeout),
		stream: httpclient.NewStreamingClient(),
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return "anthropic" }

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []Content
}
type Request struct {
	Model     string          `json:"model"`
	Messages  []Message       `json:"messages"`
	System    string          `json:"system,omitempty"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream,omitempty"`
	Thinking  *ThinkingConfig `json:"thinking,omitempty"`
}

// ThinkingConfig is Anthropic's native extended-thinking control. See
// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking.
// Type is always "enabled" when sent; BudgetTokens caps how much reasoning
// the model may do per turn.
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type Response struct {
	ID         string    `json:"id"`
	Content    []Content `json:"content"`
	Model      string    `json:"model"`
	StopReason string    `json:"stop_reason"`
	Usage      Usage     `json:"usage"`
}
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Thinking and Signature are populated when Type is "thinking" or
	// "redacted_thinking". Signature must be round-tripped unchanged on
	// subsequent requests for tool use to work.
	Thinking  string       `json:"thinking,omitempty"`
	Signature string       `json:"signature,omitempty"`
	Data      string       `json:"data,omitempty"` // "redacted_thinking" payload
	Source    *ImageSource `json:"source,omitempty"`
}
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}
type StreamEvent struct {
	Type         string   `json:"type"`
	Delta        *Delta   `json:"delta,omitempty"`
	ContentBlock *Content `json:"content_block,omitempty"`
	Index        int      `json:"index,omitempty"`
	Usage        *Usage   `json:"usage,omitempty"`
}
type Delta struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// Convert Unified -> Anthropic
func toAnthropicReq(req *api.UpstreamChatRequest) Request {
	ar := Request{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}

	if ar.MaxTokens == 0 {
		ar.MaxTokens = 4096
	}

	// Translate the normalized ReasoningConfig into Anthropic's extended-
	// thinking block. Anthropic only accepts a token budget (no effort
	// string), so map effort buckets onto budget values that roughly
	// mirror OpenRouter's percentages.
	if req.Reasoning != nil && req.Reasoning.IsEnabled() {
		budget := req.Reasoning.MaxTokens
		if budget == 0 {
			budget = effortToThinkingBudget(req.Reasoning.Effort)
		}
		if budget > 0 {
			if budget >= ar.MaxTokens {
				// Anthropic requires budget_tokens < max_tokens; give the
				// answer at least 512 tokens of breathing room.
				ar.MaxTokens = budget + 512
			}
			ar.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: budget}
		}
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

func (a *Adapter) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingUnsupported}
}

func (a *Adapter) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	ar := toAnthropicReq(req)
	ar.Stream = false

	var anthroResp Response
	headers := map[string]string{
		"X-Api-Key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if v, ok := a.config.Config["version"]; ok {
		headers["anthropic-version"] = v
	}

	url := fmt.Sprintf("%s/messages", strings.TrimRight(a.config.BaseURL, "/"))
	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, ar, &anthroResp); err != nil {
		return nil, err
	}

	content, reasoning, reasoningDetails := extractContent(anthroResp.Content)

	// Only fall back to legacy <think> tag stripping when the provider did
	// not return a native thinking block, so modern Claude models don't pay
	// for redundant post-processing.
	if reasoning == "" {
		content, reasoning = processing.ExtractThinking(content)
	}

	msg := &api.ChatMessage{
		Role:      "assistant",
		Content:   api.Content{Text: content},
		Reasoning: reasoning,
	}
	if len(reasoningDetails) > 0 {
		msg.ReasoningDetails = reasoningDetails
	}

	return &api.ChatResponse{
		ID:      anthroResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   anthroResp.Model,
		Choices: []api.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: anthroResp.StopReason,
		}},
		Usage: anthropicUsage(anthroResp.Usage),
	}, nil
}

// extractContent walks the Anthropic content-block array and separates
// visible text from thinking/redacted_thinking blocks. The reasoning
// details preserve the original block order + signatures so the agentic
// loop can replay them on the assistant turn that precedes the tool
// result (required for Claude tool use to work).
func extractContent(blocks []Content) (text string, reasoning string, details []api.ReasoningDetail) {
	var textSB, thoughtSB strings.Builder
	for i, c := range blocks {
		switch c.Type {
		case "text":
			textSB.WriteString(c.Text)
		case "thinking":
			thoughtSB.WriteString(c.Thinking)
			details = append(details, api.ReasoningDetail{
				Type:      "reasoning.text",
				Text:      c.Thinking,
				Signature: c.Signature,
				Format:    "anthropic-claude-v1",
				Index:     i,
			})
		case "redacted_thinking":
			details = append(details, api.ReasoningDetail{
				Type:   "reasoning.encrypted",
				Data:   c.Data,
				Format: "anthropic-claude-v1",
				Index:  i,
			})
		}
	}
	return textSB.String(), thoughtSB.String(), details
}

// anthropicUsage converts Anthropic's usage shape into the unified usage
// type with cache-read/cache-write breakdowns populated where available.
func anthropicUsage(u Usage) *api.ResponseUsage {
	usage := &api.ResponseUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
	}
	if u.CacheReadInputTokens > 0 || u.CacheCreationInputTokens > 0 {
		usage.PromptTokensDetails = &api.PromptTokensDetails{
			CachedTokens:     u.CacheReadInputTokens,
			CacheWriteTokens: u.CacheCreationInputTokens,
		}
	}
	return usage
}

// effortToThinkingBudget maps OpenRouter-style effort buckets onto an
// Anthropic `budget_tokens` value. Anthropic rejects budgets below 1024,
// so `minimal` rounds up to the minimum.
func effortToThinkingBudget(effort string) int {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "xhigh":
		return 32000
	case "high":
		return 16000
	case "medium":
		return 8000
	case "low":
		return 2000
	case "minimal":
		return 1024
	}
	return 0
}

func (a *Adapter) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)
	ar := toAnthropicReq(req)
	ar.Stream = true

	url := fmt.Sprintf("%s/messages", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"X-Api-Key":         a.config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if v, ok := a.config.Config["version"]; ok {
		headers["anthropic-version"] = v
	}

	go func() {
		defer close(ch)

		parser := processing.NewStreamParser()
		// Track the block type per index so we can interpret the
		// subsequent delta events correctly (text vs thinking vs
		// signature).
		blockTypes := map[int]string{}
		// Buffer thinking text per block so we can emit it as a
		// reasoning_detail on block close (when we know the signature).
		thinkingBufs := map[int]*strings.Builder{}
		thinkingSigs := map[int]string{}

		err := httpclient.StreamRequest(ctx, a.stream, "POST", url, headers, ar, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}
			data := strings.TrimPrefix(line, "data: ")

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return nil
			}

			switch event.Type {
			case "message_start":
				if event.Usage != nil {
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Usage: anthropicUsage(*event.Usage),
					}}
				}
			case "content_block_start":
				if event.ContentBlock != nil {
					blockTypes[event.Index] = event.ContentBlock.Type
					if event.ContentBlock.Type == "redacted_thinking" {
						ch <- api.StreamResult{Response: &api.ChatResponse{
							Choices: []api.Choice{{
								Delta: &api.ChatMessage{
									ReasoningDetails: []api.ReasoningDetail{{
										Type:   "reasoning.encrypted",
										Data:   event.ContentBlock.Data,
										Format: "anthropic-claude-v1",
										Index:  event.Index,
									}},
								},
							}},
						}}
					}
				}
			case "content_block_delta":
				if event.Delta == nil {
					return nil
				}
				switch event.Delta.Type {
				case "text_delta":
					c, r := parser.Process(event.Delta.Text)
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Choices: []api.Choice{{
							Delta: &api.ChatMessage{
								Content:   api.Content{Text: c},
								Reasoning: r,
							},
						}},
					}}
				case "thinking_delta":
					// Stream the thought text to the client as reasoning
					// and also buffer it so we can emit a typed detail
					// block with the signature at block close.
					buf, ok := thinkingBufs[event.Index]
					if !ok {
						buf = &strings.Builder{}
						thinkingBufs[event.Index] = buf
					}
					buf.WriteString(event.Delta.Thinking)
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Choices: []api.Choice{{
							Delta: &api.ChatMessage{
								Reasoning: event.Delta.Thinking,
							},
						}},
					}}
				case "signature_delta":
					// Anthropic sends the signature once per thinking
					// block after its text has streamed; store it so we
					// can attach it on block close.
					thinkingSigs[event.Index] += event.Delta.Signature
				}
			case "content_block_stop":
				// Flush the buffered thinking block as a reasoning_detail
				// so the caller can replay it verbatim on the next turn.
				if buf, ok := thinkingBufs[event.Index]; ok {
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Choices: []api.Choice{{
							Delta: &api.ChatMessage{
								ReasoningDetails: []api.ReasoningDetail{{
									Type:      "reasoning.text",
									Text:      buf.String(),
									Signature: thinkingSigs[event.Index],
									Format:    "anthropic-claude-v1",
									Index:     event.Index,
								}},
							},
						}},
					}}
					delete(thinkingBufs, event.Index)
					delete(thinkingSigs, event.Index)
				}
				delete(blockTypes, event.Index)
			case "message_delta":
				if event.Usage != nil {
					ch <- api.StreamResult{Response: &api.ChatResponse{
						Usage: &api.ResponseUsage{
							CompletionTokens: event.Usage.OutputTokens,
						},
					}}
				}
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


func (a *Adapter) Health(ctx context.Context) error {
	// Anthropic's "list models" endpoint is a good candidate for a health check
	// as it requires auth and verifies connectivity.
	url := "https://api.anthropic.com/v1/models?limit=1"
	if a.config.BaseURL != "" {
		url = fmt.Sprintf("%s/models?limit=1", strings.TrimRight(a.config.BaseURL, "/"))
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
