package openai

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
	llm.Register("openai", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
	stream *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	fmt.Printf("DEBUG: OpenAI Adapter Init. ID=%s BaseURL='%s' APIKeyLen=%d\n", config.ID, config.BaseURL, len(config.APIKey))
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
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
	return "openai"
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

func (a *Adapter) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat}
}

func (a *Adapter) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	var resp api.ChatResponse
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	req.Stream = false

	payload := a.buildUpstreamPayload(req)

	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, payload, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	// OpenAI-compat providers return reasoning in a few shapes:
	//   * DeepSeek, Qwen, Moonshot: `message.reasoning_content` (aliased
	//     into Reasoning by ChatMessage.UnmarshalJSON already).
	//   * OpenRouter, xAI: `message.reasoning`.
	//   * Legacy/non-compliant: `<think>...</think>` tags inside content.
	// We only fall back to the tag-extraction heuristic when the upstream
	// didn't populate a dedicated field, so a reasoning-aware provider
	// doesn't pay for redundant post-processing.
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if choice.Message == nil {
			continue
		}
		if choice.Message.Reasoning != "" {
			continue
		}
		content, reasoning := processing.ExtractThinking(choice.Message.Content.Text)
		if reasoning != "" {
			choice.Message.Content.Text = content
			choice.Message.Reasoning = reasoning
		}
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	req.Stream = true
	req.StreamOptions = &api.StreamOptions{IncludeUsage: true}
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	payload := a.buildUpstreamPayload(req)

	go func() {
		defer close(ch)

		// Per-choice parsers that handle the legacy `<think>` tag case. We
		// only route content through the parser when the provider hasn't
		// already populated a first-class reasoning field for the same
		// chunk; this keeps the fast path (modern providers) free of
		// any extra string work.
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
				if choice.Delta == nil {
					continue
				}

				// If the provider sent a native reasoning delta (either
				// `reasoning` or `reasoning_content`, aliased by the
				// ChatMessage unmarshaller), skip the tag-scanner entirely.
				if choice.Delta.Reasoning != "" {
					continue
				}

				idx := choice.Index
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

// upstreamPayload mirrors the subset of UpstreamChatRequest that
// OpenAI-compatible providers accept plus the native reasoning controls
// (reasoning_effort and, when the base URL looks like OpenRouter, the
// structured `reasoning` object). We purposely do NOT forward the generic
// `reasoning` field by default because strict OpenAI-compatible shims
// (Gemini, DeepSeek direct, Moonshot) will 400 on unknown fields.
type upstreamPayload struct {
	*api.UpstreamChatRequest
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	Reasoning       *upstreamReasoning `json:"reasoning,omitempty"`
}

type upstreamReasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
}

// buildUpstreamPayload translates the router's normalized ReasoningConfig
// into the native shape each OpenAI-compatible upstream actually accepts.
// It never mutates the caller's request. When reasoning is not requested
// the original request is returned verbatim so the JSON encoder takes
// the zero-copy fast path.
func (a *Adapter) buildUpstreamPayload(req *api.UpstreamChatRequest) any {
	if req == nil {
		return req
	}
	r := req.Reasoning
	// Always strip the router-only Reasoning field before serialization;
	// non-OpenRouter upstreams reject unknown fields and we re-add the
	// native equivalents below when a reasoning request is actually active.
	inner := *req
	inner.Reasoning = nil
	if r == nil || (!r.IsEnabled() && !r.Exclude) {
		return &inner
	}

	out := upstreamPayload{UpstreamChatRequest: &inner}

	if r.Effort != "" {
		out.ReasoningEffort = normalizeEffort(r.Effort)
	}

	// Pass the full object through only to upstreams that we know accept
	// it (OpenRouter and other gateway-style endpoints). This check is
	// deliberately loose — exact hostname matching is brittle across self-
	// hosted OpenRouter deployments.
	if strings.Contains(a.config.BaseURL, "openrouter") {
		out.Reasoning = &upstreamReasoning{
			Effort:    r.Effort,
			MaxTokens: r.MaxTokens,
			Exclude:   r.Exclude,
		}
	}

	return out
}

// normalizeEffort clamps OpenRouter-style effort values to the subset OpenAI
// actually accepts. `xhigh` / `none` are OpenRouter extensions; we map them
// to the nearest valid bucket rather than drop the field.
func normalizeEffort(e string) string {
	switch strings.ToLower(strings.TrimSpace(e)) {
	case "xhigh", "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	case "minimal":
		return "minimal"
	case "none":
		// OpenAI has no explicit off-switch; omit the header by returning "".
		return ""
	default:
		return "medium"
	}
}


func (a *Adapter) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/models", strings.TrimRight(a.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

	if org, ok := a.config.Config["organization"]; ok {
		req.Header.Set("OpenAI-Organization", org)
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
