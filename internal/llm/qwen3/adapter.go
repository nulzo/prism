package qwen3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register("qwen3", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		// Default to local vLLM-Omni server
		config.BaseURL = "http://localhost:8000/v1"
	}

	transport := &http.Transport{
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 500,
		MaxConnsPerHost:     500,
		IdleConnTimeout:     90 * time.Second,
	}

	timeout := 10 * time.Minute
	if config.Timeout != "" {
		if d, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = d
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
	return "qwen3"
}

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	// Qwen3-TTS via vLLM-Omni uses the standard OpenAI chat/completions format
	// with modalities and audio configuration.
	
	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))
	
	// Ensure modalities are set for audio
	hasAudio := false
	for _, m := range req.Modalities {
		if m == "audio" {
			hasAudio = true
			break
		}
	}
	
	if !hasAudio {
		req.Modalities = append(req.Modalities, "audio")
	}
	
	if req.Audio == nil {
		req.Audio = &api.AudioConfig{
			Voice:  "default",
			Format: "wav",
		}
	}

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if a.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qwen3 error: %s", string(body))
	}

	var chatResp api.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, err
	}

	return &chatResp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	// For simplicity, we'll just call the non-streaming endpoint and return it as one chunk
	// A full implementation would stream the SSE response.
	
	ch := make(chan api.StreamResult)
	
	go func() {
		defer close(ch)
		resp, err := a.Chat(ctx, req)
		if err != nil {
			ch <- api.StreamResult{Err: err}
			return
		}
		
		resp.Object = "chat.completion.chunk"
		if len(resp.Choices) > 0 {
			resp.Choices[0].Delta = resp.Choices[0].Message
			resp.Choices[0].Message = nil
		}
		
		ch <- api.StreamResult{Response: resp}
	}()
	
	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	return a.config.StaticModels, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	return nil
}
