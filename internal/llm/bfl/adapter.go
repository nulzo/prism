package bfl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
)

const pn string = "bfl"

func init() {
	llm.Register(pn, NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.bfl.ai/v1"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 300 * time.Second}, // Long timeout for generation + polling
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return pn }

// Request structures
type GenerationRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type GenerationResponse struct {
	ID         string `json:"id"`
	PollingURL string `json:"polling_url"`
}

type PollingResult struct {
	Sample string `json:"sample"`
}

type PollingResponse struct {
	Status  string         `json:"status"` // Ready, Processing, Pending, Error, Failed
	Result  *PollingResult `json:"result,omitempty"`
	Message string         `json:"message,omitempty"`
}

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {

	prompt := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == string(api.User) {
			// Extract text from Content
			if req.Messages[i].Content.Text != "" {
				prompt = req.Messages[i].Content.Text
			} else {
				for _, p := range req.Messages[i].Content.Parts {
					if p.Type == "text" {
						prompt += p.Text
					}
				}
			}
			break
		}
	}

	if prompt == "" {
		return nil, fmt.Errorf("no prompt found in messages")
	}

	bflReq := GenerationRequest{
		Prompt: prompt,
		Width:  1024,
		Height: 1024,
	}

	endpoint := fmt.Sprintf("%s/%s", a.config.BaseURL, req.Model)

	reqBody, err := json.Marshal(bflReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("x-key", a.config.APIKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("BFL API error: status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var genResp GenerationResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	pollingURL := genResp.PollingURL
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var finalImageURL string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// Poll
			pollReq, err := http.NewRequestWithContext(ctx, "GET", pollingURL, nil)
			if err != nil {
				return nil, err
			}
			pollReq.Header.Set("accept", "application/json")
			pollReq.Header.Set("x-key", a.config.APIKey)

			pollResp, err := a.client.Do(pollReq)
			if err != nil {
				return nil, fmt.Errorf("polling failed: %w", err)
			}
			defer pollResp.Body.Close()

			var pollResult PollingResponse
			// Read body for debugging if decode fails
			bodyBytes, _ := io.ReadAll(pollResp.Body)
			if err := json.Unmarshal(bodyBytes, &pollResult); err != nil {
				return nil, fmt.Errorf("failed to decode polling response: %w", err)
			}

			if pollResult.Status == "Ready" {
				if pollResult.Result != nil {
					finalImageURL = pollResult.Result.Sample
				}
				goto DonePolling
			} else if pollResult.Status == "Error" || pollResult.Status == "Failed" {
				return nil, fmt.Errorf("generation failed: %s", pollResult.Message)
			}
			// Continue polling if "Processing" or "Pending"
		}
	}

DonePolling:

	return &api.ChatResponse{
		ID:      genResp.ID,
		Model:   req.Model,
		Created: time.Now().Unix(),
		Choices: []api.Choice{{
			Index: 0,
			Message: &api.ChatMessage{
				Role: "assistant",
				Content: api.Content{
					Parts: []api.ContentPart{
						{
							Type: "image_url",
							ImageURL: &api.ImageURL{
								URL: finalImageURL,
							},
						},
					},
				},
				Images: []api.ContentPart{
					{
						Type: "image_url",
						ImageURL: &api.ImageURL{
							URL: finalImageURL,
						},
					},
				},
			},
			FinishReason: "stop",
		}},
		Usage: &api.ResponseUsage{
			TotalTokens: 0,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {

	ch := make(chan api.StreamResult)
	go func() {
		defer close(ch)
		resp, err := a.Chat(ctx, req)
		if err != nil {
			ch <- api.StreamResult{Err: err}
			return
		}
		ch <- api.StreamResult{Response: resp}
	}()
	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	return a.config.StaticModels, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	if a.config.APIKey == "" {
		return fmt.Errorf("missing API key")
	}
	return nil
}
