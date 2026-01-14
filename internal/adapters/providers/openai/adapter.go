package openai

import (
	"bufio"
	"bytes"
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

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	if org, ok := a.config.Config["organization"]; ok {
		httpReq.Header.Set("OpenAI-Organization", org)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("stream connection failed: %s", resp.Status)
	}

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			
			// SSE format: data: {...}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var chatResp schema.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				// Don't fail the whole stream for one bad chunk, but maybe log it
				continue 
			}

			ch <- ports.StreamResult{Response: &chatResp}
		}
		
		if err := scanner.Err(); err != nil {
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
