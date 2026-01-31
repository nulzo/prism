package ollama

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/openai"
	"github.com/nulzo/model-router-api/pkg/api"
)

type Capability string

const (
	CapabilityCompletion = Capability("completion")
	CapabilityTools      = Capability("tools")
	CapabilityInsert     = Capability("insert")
	CapabilityVision     = Capability("vision")
	CapabilityEmbedding  = Capability("embedding")
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

	var (
		models []api.ModelDefinition
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	// Limit concurrency to avoid overwhelming the local Ollama instance
	semaphore := make(chan struct{}, 5)
	showURL := fmt.Sprintf("%s/api/show", rootURL)

	for _, m := range resp.Models {
		wg.Add(1)
		go func(mName string, mSize int64) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Default values
			isMultimodal := false
			hasTools := false
			contextLength := 4096

			var showResp struct {
				Details struct {
					Families      []string `json:"families"`
					Family        string   `json:"family"`
					ParameterSize string   `json:"parameter_size"`
				} `json:"details"`
				ModelInfo    map[string]interface{} `json:"model_info"`
				Capabilities []Capability           `json:"capabilities"`
			}

			reqBody := map[string]string{"model": mName}

			// we can ignore errors here and default to basic config to avoid breaking the whole list
			if err := httpclient.SendRequest(ctx, a.client, "POST", showURL, nil, reqBody, &showResp); err == nil {
				// only newer versions of ollama supports this, so we can default to checking and fallback if needed
				for _, cap := range showResp.Capabilities {
					switch cap {
					case CapabilityTools:
						hasTools = true
					case CapabilityVision:
						isMultimodal = true
					}
				}

				// if capabilities not in api (older version) just try to manually determine
				for _, f := range showResp.Details.Families {
					if f == "clip" || f == "mllama" {
						isMultimodal = true
						break
					}
				}
				if !isMultimodal && (showResp.Details.Family == "clip" || showResp.Details.Family == "mllama") {
					isMultimodal = true
				}

				if showResp.ModelInfo != nil {
					for k, v := range showResp.ModelInfo {
						if strings.Contains(k, "context_length") {
							if f, ok := v.(float64); ok {
								contextLength = int(f)
								break
							}
						}
					}
				}
			}

			modalities := []string{"text"}
			if isMultimodal {
				modalities = append(modalities, "image")
			}

			modelDef := api.ModelDefinition{
				ID:            fmt.Sprintf("%s/%s", string(llm.Ollama), mName),
				Name:          mName,
				ProviderID:    a.config.ID,
				UpstreamID:    mName,
				Description:   fmt.Sprintf("Ollama model (Size: %d bytes)", mSize),
				Enabled:       true,
				Source:        "auto",
				LastUpdated:   time.Now(),
				ContextLength: contextLength,
				Pricing: api.ModelPricing{
					Prompt:     "0",
					Completion: "0",
					Request:    "0",
					Image:      "0",
					WebSearch:  "0",
				},
				Config: api.ModelConfig{
					ContextWindow:    contextLength,
					MaxOutput:        4096,
					Modality:         modalities,
					ImageSupport:     isMultimodal,
					ToolUse:          hasTools,
					StreamingSupport: true,
				},
				Architecture: api.ModelArchitecture{
					InputModalities:  modalities,
					OutputModalities: []string{"text"},
					Tokenizer:        "ollama",
					InstructType:     "",
				},
				TopProvider: api.ModelTopProvider{
					ContextLength:       contextLength,
					MaxCompletionTokens: 4096,
					IsModerated:         false,
				},
			}

			mu.Lock()
			models = append(models, modelDef)
			mu.Unlock()
		}(m.Name, m.Size)
	}

	wg.Wait()
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
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	return nil
}
