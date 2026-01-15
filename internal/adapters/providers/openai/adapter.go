package openai

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

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	var resp schema.ChatResponse
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}
	
	// Handle organization header if present in config
	if org, ok := a.config.Config["organization"]; ok {
		headers["OpenAI-Organization"] = org
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))
	
	// Ensure stream is false for this method
	req.Stream = false
	
	if err := utils.SendRequest(ctx, a.client, "POST", url, headers, req, &resp); err != nil {
		return nil, err
	}
	
	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	ch := make(chan ports.StreamResult)

	// Ensure stream is true
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
				return nil // We can't return special error to stop, loop continues until end of body or context cancel
			}

			var chatResp schema.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				// Log error but continue
				return nil
			}

			ch <- ports.StreamResult{Response: &chatResp}
			return nil
		})

		if err != nil {
			ch <- ports.StreamResult{Err: err}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	var resp struct {
		Data []schema.Model `json:"data"`
	}
	
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	url := fmt.Sprintf("%s/models", strings.TrimRight(a.config.BaseURL, "/"))
	
	if err := utils.SendRequest(ctx, a.client, "GET", url, headers, nil, &resp); err != nil {
		return nil, err
	}
	
	// Enrich with provider ID
	for i := range resp.Data {
		resp.Data[i].Provider = a.config.ID
	}

	return resp.Data, nil
}
