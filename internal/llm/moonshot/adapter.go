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
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register("moonshot", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	fmt.Printf("DEBUG: Moonshot Adapter Init. ID=%s BaseURL='%s' APIKeyLen=%d\n", config.ID, config.BaseURL, len(config.APIKey))
	if config.BaseURL == "" {
		config.BaseURL = "https://api.moonshot.ai/v1"
	}

	// Use a custom transport to support high concurrency
	transport := &http.Transport{
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 500,
		MaxConnsPerHost:     500, // Limit total connections to prevent storm
		IdleConnTimeout:     90 * time.Second,
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
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

func (a *Adapter) Name() string {
	return a.config.ID
}

func (a *Adapter) Type() string {
	return "moonshot"
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

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	// ensure stream is false for this method
	req.Stream = false

	// Moonshot uses max_completion_tokens instead of max_tokens
	if req.MaxTokens > 0 {
		req.MaxCompletionTokens = req.MaxTokens
		req.MaxTokens = 0
	}

	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, req, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	// Post-process to extract thinking content
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if choice.Message != nil {
			content, reasoning := processing.ExtractThinking(choice.Message.Content.Text)
			choice.Message.Content.Text = content
			choice.Message.Reasoning = reasoning
		}
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	// ensure stream is true
	req.Stream = true
	req.StreamOptions = &api.StreamOptions{IncludeUsage: true}
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))

	// Moonshot uses max_completion_tokens instead of max_tokens
	if req.MaxTokens > 0 {
		req.MaxCompletionTokens = req.MaxTokens
		req.MaxTokens = 0
	}

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	go func() {
		defer close(ch)

		// Map of parsers for each choice index
		parsers := make(map[int]*processing.StreamParser)

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
					c, r := parser.Process(choice.Delta.Content.Text)
					choice.Delta.Content.Text = c
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

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	url := fmt.Sprintf("%s/models", strings.TrimRight(a.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return a.config.StaticModels, nil
	}

	req.Header.Set("Authorization", "Bearer "+a.config.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return a.config.StaticModels, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return a.config.StaticModels, nil
	}

	var upstreamResp struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&upstreamResp); err != nil {
		return a.config.StaticModels, nil
	}

	// Create a map of existing models for quick lookup
	existingModels := make(map[string]bool)
	for _, m := range a.config.StaticModels {
		existingModels[m.UpstreamID] = true
	}

	mergedModels := make([]api.ModelDefinition, len(a.config.StaticModels))
	copy(mergedModels, a.config.StaticModels)

	// Check for new models
	for _, upstreamModel := range upstreamResp.Data {
		if !existingModels[upstreamModel.ID] {
			logger.Warn(fmt.Sprintf("Provider '%s' has a new model available upstream that is not in config: %s", a.config.ID, upstreamModel.ID))
			
			// Add it with default/empty pricing so it's usable
			newModel := api.ModelDefinition{
				ID:          fmt.Sprintf("%s/%s", a.config.ID, upstreamModel.ID),
				Name:        upstreamModel.ID,
				ProviderID:  a.config.ID,
				UpstreamID:  upstreamModel.ID,
				Enabled:     true,
				ContextLength: upstreamModel.ContextLength,
				Pricing: api.ModelPricing{
					Prompt:     "0",
					Completion: "0",
				},
			}
			mergedModels = append(mergedModels, newModel)
		}
	}

	return mergedModels, nil
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
