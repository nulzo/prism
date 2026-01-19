package ollama

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/openai"
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register(string(llm.Ollama), NewAdapter)
}

type Adapter struct {
	llm.Provider // embeds the OpenAI adapter for chat/stream capabilities
	config       config.ProviderConfig
	client       *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if !strings.HasSuffix(config.BaseURL, "/v1") {
		config.BaseURL = strings.TrimRight(config.BaseURL, "/") + "/v1"
	}

	oaAdapter, err := openai.NewAdapter(config)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		Provider: oaAdapter,
		config:   config,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	// Ollama is dynamic, so we query the API and map to ModelDefinition
	rootURL := a.config.BaseURL
	rootURL = strings.TrimSuffix(strings.TrimRight(rootURL, "/"), "/v1")
	url := fmt.Sprintf("%s/api/tags", rootURL)

	var resp struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
			Size       int64  `json:"size"`
		} `json:"models"`
	}

	if err := httpclient.SendRequest(ctx, a.client, "GET", url, nil, nil, &resp); err != nil {
		var upstreamErr *httpclient.UpstreamError
		if errors.As(err, &upstreamErr) {
			return nil, api.NewError(
				upstreamErr.StatusCode,
				"Ollama Registry Error",
				string(upstreamErr.Body),
				api.WithLog(err),
			)
		}
		return nil, fmt.Errorf("ollama tags error: %w", err)
	}

	var models []api.ModelDefinition
	for _, m := range resp.Models {

		modelDef := api.ModelDefinition{
			ID:          fmt.Sprintf("%s/%s", string(llm.Ollama), m.Name),
			Name:        m.Name,
			ProviderID:  a.config.ID,
			UpstreamID:  m.Name,
			Description: fmt.Sprintf("Ollama model (Size: %d bytes)", m.Size),
			Enabled:     true,
			Source:      "auto",
			LastUpdated: time.Now(),
			Pricing: api.ModelPricing{
				Input:  0,
				Output: 0,
				Image:  0,
			},
			Config: api.ModelConfig{
				ContextWindow:    4096, // Default assumption, can't easily know from tags
				MaxOutput:        4096,
				Modality:         []string{"text"},
				ImageSupport:     false, // TODO: detect if multimodal
				ToolUse:          false,
				StreamingSupport: true,
			},
		}
		models = append(models, modelDef)
	}

	return models, nil
}

func (a *Adapter) Type() string {
	return string(llm.Ollama)
}
