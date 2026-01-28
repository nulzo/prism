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
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register("openai", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
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

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	var resp api.ChatResponse
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

	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, req, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	// ensure stream is true
	req.Stream = true
	req.StreamOptions = &api.StreamOptions{IncludeUsage: true}
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	go func() {
		defer close(ch)

		err := httpclient.StreamRequest(ctx, a.client, "POST", url, headers, req, func(line string) error {
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

			ch <- api.StreamResult{Response: &chatResp}
			return nil
		})

		if err != nil {
			ch <- api.StreamResult{Err: a.handleUpstreamError(err)}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {

	// OpenAI provider now uses static configuration as the source of truth

	// because the API does not provide pricing or detailed capability flags.

	return a.config.StaticModels, nil

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

	defer resp.Body.Close()



	if resp.StatusCode != http.StatusOK {

		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)

	}



	return nil

}
