package ollama

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/adapters/providers/openai"
	"github.com/nulzo/model-router-api/internal/adapters/providers/utils"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/registry"
	"github.com/nulzo/model-router-api/pkg/schema"
)

func init() {
	registry.Register("ollama", NewAdapter)
}

type Adapter struct {
	ports.ModelProvider // Embeds the OpenAI adapter for Chat/Stream capabilities
	config              domain.ProviderConfig
	client              *http.Client
}

func NewAdapter(config domain.ProviderConfig) (ports.ModelProvider, error) {
	// 1. Setup OpenAI Adapter for Chat/Stream (needs /v1)
	openAIConfig := config
	if openAIConfig.BaseURL == "" {
		openAIConfig.BaseURL = "http://localhost:11434/v1"
	} else if !strings.HasSuffix(openAIConfig.BaseURL, "/v1") {
		openAIConfig.BaseURL = strings.TrimRight(openAIConfig.BaseURL, "/") + "/v1"
	}

	oaAdapter, err := openai.NewAdapter(openAIConfig)
	if err != nil {
		return nil, err
	}

	// 2. Return our wrapper
	return &Adapter{
		ModelProvider: oaAdapter,
		config:        config,
		client:        &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Models overrides the OpenAI implementation to hit the native Ollama /api/tags endpoint
func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	// Clean the BaseURL to get the root (remove /v1 if present)
	rootURL := a.config.BaseURL
	if rootURL == "" {
		rootURL = "http://localhost:11434"
	}
	rootURL = strings.TrimSuffix(strings.TrimRight(rootURL, "/"), "/v1")
	
	url := fmt.Sprintf("%s/api/tags", rootURL)

	var resp struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
			Size       int64  `json:"size"`
		} `json:"models"`
	}

	if err := utils.SendRequest(ctx, a.client, "GET", url, nil, nil, &resp); err != nil {
		return nil, fmt.Errorf("ollama tags error: %w", err)
	}

	var models []schema.Model
	for _, m := range resp.Models {
		models = append(models, schema.Model{
			ID:          m.Name,
			Name:        m.Name,
			Provider:    a.config.ID,
			OwnedBy:     "ollama",
			Description: fmt.Sprintf("Ollama model (Size: %d bytes)", m.Size),
		})
	}

	return models, nil
}

func (a *Adapter) Type() string {
	return "ollama"
}
