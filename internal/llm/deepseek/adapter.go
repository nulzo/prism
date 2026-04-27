package deepseek

import (
	"context"
	"encoding/json"
	"errors"
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
	llm.Register("deepseek", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
	stream *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.deepseek.com"
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

func (a *Adapter) Name() string {
	return a.config.ID
}

func (a *Adapter) Type() string {
	return "deepseek"
}

func (a *Adapter) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat}
}

// upstreamErrorResponse mirrors the standard OpenAI error shape
type upstreamErrorResponse struct {
	Error struct {
		Message string      `json:"message"`
		Type    string      `json:"type"`
		Param   interface{} `json:"param"`
		Code    interface{} `json:"code"`
	} `json:"error"`
}

func (a *Adapter) handleUpstreamError(err error) error {
	var upstreamErr *httpclient.UpstreamError
	if !errors.As(err, &upstreamErr) {
		return err
	}

	// parse the specific upstream error format
	var apiErr upstreamErrorResponse
	if jsonErr := json.Unmarshal(upstreamErr.Body, &apiErr); jsonErr != nil {
		// if we can't parse it, return a generic upstream error
		return api.NewError(
			upstreamErr.StatusCode,
			"Upstream Error",
			string(upstreamErr.Body),
			api.WithLog(err),
		)
	}

	// create a nice RFC 9457 problem
	return api.NewError(
		upstreamErr.StatusCode,
		"Upstream Provider Error",
		apiErr.Error.Message,
		api.WithType("about:blank"),
		api.WithExtension("upstream_code", apiErr.Error.Code),
		api.WithExtension("upstream_type", apiErr.Error.Type),
		api.WithExtension("upstream_param", apiErr.Error.Param),
		api.WithLog(err),
	)
}

type thinkingConfig struct {
	Type string `json:"type"`
}

type upstreamPayload struct {
	*api.UpstreamChatRequest
	Messages        []processing.CompatMessage `json:"messages"`
	Thinking        *thinkingConfig            `json:"thinking,omitempty"`
	ReasoningEffort string                     `json:"reasoning_effort,omitempty"`
}

func (a *Adapter) buildUpstreamPayload(req *api.UpstreamChatRequest) any {
	if req == nil {
		return req
	}

	r := req.Reasoning
	inner := *req
	inner.Reasoning = nil // Strip router-only Reasoning field

	out := upstreamPayload{
		UpstreamChatRequest: &inner,
		Messages:            processing.FormatOpenAIMessages(inner.Messages),
	}

	if r == nil || (!r.IsEnabled() && !r.Exclude) {
		return out
	}

	// DeepSeek supports "thinking: {type: 'enabled'}" and "reasoning_effort"
	out.Thinking = &thinkingConfig{Type: "enabled"}

	if r.Effort != "" {
		effort := strings.ToLower(strings.TrimSpace(r.Effort))
		if effort == "xhigh" || effort == "high" {
			out.ReasoningEffort = "high"
		} else if effort == "low" || effort == "minimal" {
			out.ReasoningEffort = "low"
		} else {
			out.ReasoningEffort = "medium"
		}
	}

	return out
}

func (a *Adapter) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	var resp api.ChatResponse
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	// ensure stream is false for this method
	req.Stream = false

	payload := a.buildUpstreamPayload(req)

	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, payload, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	// Post-process to extract thinking content
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if choice.Message != nil {
			if choice.Message.Reasoning != "" {
				continue
			}
			content, reasoning := processing.ExtractThinking(choice.Message.Content.Text)
			if reasoning != "" {
				choice.Message.Content.Text = content
				choice.Message.Reasoning = reasoning
			}
		}
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	// ensure stream is true
	req.Stream = true
	req.StreamOptions = &api.StreamOptions{IncludeUsage: true}
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	payload := a.buildUpstreamPayload(req)

	go func() {
		defer close(ch)

		// Map of parsers for each choice index
		parsers := make(map[int]*processing.StreamParser)

		err := httpclient.StreamRequest(ctx, a.stream, "POST", url, headers, payload, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil
			}

			var chatResp api.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				return nil
			}

			for i := range chatResp.Choices {
				choice := &chatResp.Choices[i]
				idx := choice.Index

				if choice.Delta == nil {
					continue
				}

				if choice.Delta.Reasoning != "" {
					continue
				}

				parser, ok := parsers[idx]
				if !ok {
					parser = processing.NewStreamParser()
					parsers[idx] = parser
				}

				c, r := parser.Process(choice.Delta.Content.Text)
				choice.Delta.Content.Text = c
				if r != "" {
					choice.Delta.Reasoning = r
				}
			}

			ch <- api.StreamResult{Response: &chatResp}
			return nil
		})
		if err != nil {
			ch <- api.StreamResult{Err: a.handleUpstreamError(err)}
		}
	}()

	return ch, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/models", strings.TrimRight(a.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

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
