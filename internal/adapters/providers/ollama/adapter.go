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

	return &Adapter{
		ModelProvider: oaAdapter,
		config:        config,
		client:        &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
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
			ID:          fmt.Sprintf("%s/%s", "ollama", m.Name),
			Name:        m.Name,
			Provider:    a.config.ID,
			OwnedBy:     "ollama",
			Description: fmt.Sprintf("Ollama model (Size: %d bytes)", m.Size),
			// ollama is always free
			Pricing: schema.Pricing{
				Prompt:     "0",
				Completion: "0",
				Image:      "0",
				Request:    "0",
			},
		})
	}

	return models, nil
}

func (a *Adapter) Type() string {
	return "ollama"
}
