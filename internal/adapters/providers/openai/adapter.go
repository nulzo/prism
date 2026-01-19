package openai

import (
	"context"
	"encoding/json"
	"errors"
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
	registry.Register("openai", NewAdapter)
}

type Adapter struct {
	config domain.ProviderConfig
	client *http.Client
}

func NewAdapter(config domain.ProviderConfig) (ports.ModelProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
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
	var upstreamErr *domain.UpstreamError
	if !errors.As(err, &upstreamErr) {
		return err
	}

	// parse the specific upstream error format
	var apiErr upstreamErrorResponse
	if jsonErr := json.Unmarshal(upstreamErr.Body, &apiErr); jsonErr != nil {
		// if we can't parse it, return a generic upstream error
		return domain.New(
			upstreamErr.StatusCode,
			"Upstream Error",
			string(upstreamErr.Body),
			domain.WithLog(err),
		)
	}

	// create a nice RFC 9457 problem
	return domain.New(
		upstreamErr.StatusCode,
		"Upstream Provider Error",
		apiErr.Error.Message,
		domain.WithType("about:blank"),
		domain.WithExtension("upstream_code", apiErr.Error.Code),
		domain.WithExtension("upstream_type", apiErr.Error.Type),
		domain.WithExtension("upstream_param", apiErr.Error.Param),
		domain.WithLog(err),
	)
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	var resp schema.ChatResponse
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	// handle headers if present in config
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	// ensure stream is false for this method
	req.Stream = false

	if err := utils.SendRequest(ctx, a.client, "POST", url, headers, req, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	ch := make(chan ports.StreamResult)

	// ensure stream is true
	req.Stream = true
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	go func() {
		defer close(ch)

		err := utils.StreamRequest(ctx, a.client, "POST", url, headers, req, func(line string) error {
			// SSE format: data: {...}
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil // we can't return special error to stop, loop continues until end of body or context cancel
			}

			var chatResp schema.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				// log error but continue
				return nil
			}

			ch <- ports.StreamResult{Response: &chatResp}
			return nil
		})

		if err != nil {
			ch <- ports.StreamResult{Err: a.handleUpstreamError(err)}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]schema.ModelDefinition, error) {
	// OpenAI provider now uses static configuration as the source of truth
	// because the API does not provide pricing or detailed capability flags.
	return a.config.StaticModels, nil
}
