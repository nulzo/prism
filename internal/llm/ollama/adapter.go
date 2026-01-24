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
	rootURL := a.config.BaseURL
	rootURL = strings.TrimSuffix(strings.TrimRight(rootURL, "/"), "/v1")
	tagsURL := fmt.Sprintf("%s/api/tags", rootURL)

	var resp struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
			Size       int64  `json:"size"`
		} `json:"models"`
	}

	if err := httpclient.SendRequest(ctx, a.client, "GET", tagsURL, nil, nil, &resp); err != nil {
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
	showURL := fmt.Sprintf("%s/api/show", rootURL)

	for _, m := range resp.Models {
		// Detect capabilities via /api/show
		isMultimodal := false
		
		var showResp struct {
			Details struct {
				Families []string `json:"families"`
				Family   string   `json:"family"`
			} `json:"details"`
		}
		
		reqBody := map[string]string{"name": m.Name}
		// We ignore errors here and default to non-multimodal to avoid breaking the whole list
		if err := httpclient.SendRequest(ctx, a.client, "POST", showURL, reqBody, nil, &showResp); err == nil {
			// Check families for vision capabilities
			for _, f := range showResp.Details.Families {
				if f == "clip" || f == "mllama" {
					isMultimodal = true
					break
				}
			}
			// Fallback to single family check
			if !isMultimodal && (showResp.Details.Family == "clip" || showResp.Details.Family == "mllama") {
				isMultimodal = true
			}
		}

		modalities := []string{"text"}
		if isMultimodal {
			modalities = append(modalities, "image")
		}

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
				ContextWindow:    4096, // Default assumption
				MaxOutput:        4096,
				Modality:         modalities,
				ImageSupport:     isMultimodal,
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

func (a *Adapter) Health(ctx context.Context) error {
	rootURL := a.config.BaseURL
	rootURL = strings.TrimSuffix(strings.TrimRight(rootURL, "/"), "/v1")
	url := fmt.Sprintf("%s/api/version", rootURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
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