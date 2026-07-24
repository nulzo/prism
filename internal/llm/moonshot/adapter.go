package moonshot

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
	llm.Register("moonshot", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
	stream *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	fmt.Printf("DEBUG: Moonshot Adapter Init. ID=%s BaseURL='%s' APIKeyLen=%d\n", config.ID, config.BaseURL, len(config.APIKey))
	if config.BaseURL == "" {
		config.BaseURL = "https://api.moonshot.ai/v1"
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
		stream: httpclient.NewStreamingClient(timeout),
	}, nil
}

func (a *Adapter) Name() string {
	return a.config.ID
}

func (a *Adapter) Type() string {
	return "moonshot"
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

type upstreamPayload struct {
	*api.UpstreamChatRequest
	Messages []processing.CompatMessage `json:"messages"`
}

func (a *Adapter) buildUpstreamPayload(req *api.UpstreamChatRequest) any {
	if req == nil {
		return req
	}

	// Moonshot uses max_completion_tokens instead of max_tokens
	if req.MaxTokens > 0 {
		req.MaxCompletionTokens = req.MaxTokens
		req.MaxTokens = 0
	}

	inner := *req
	inner.Reasoning = nil // Strip router-only Reasoning field

	out := upstreamPayload{
		UpstreamChatRequest: &inner,
		Messages:            processing.FormatOpenAIMessages(inner.Messages),
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

	payload := a.buildUpstreamPayload(req)

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	go func() {
		defer close(ch)

		// Map of parsers for each choice index
		parsers := make(map[int]*processing.StreamParser)

		err := httpclient.StreamRequest(ctx, a.stream, "POST", url, headers, payload, func(line string) error {
			// SSE format: data: {...}
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil // we can't return special error to stop, loop continues until end of body or context cancel
			}

			var chatResp api.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				// log error but continue
				return nil
			}

			// Process thinking/reasoning tags
			for i := range chatResp.Choices {
				choice := &chatResp.Choices[i]
				idx := choice.Index

				parser, ok := parsers[idx]
				if !ok {
					parser = processing.NewStreamParser()
					parsers[idx] = parser
				}

				if choice.Delta != nil {
					if choice.Delta.Reasoning != "" {
						continue
					}
					c, r := parser.Process(choice.Delta.Content.Text)
					choice.Delta.Content.Text = c
					if r != "" {
						choice.Delta.Reasoning = r
					}
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
