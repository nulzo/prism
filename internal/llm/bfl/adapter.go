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
	"github.com/nulzo/model-router-api/internal/llm/processing"
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

	timeout := 5 * time.Minute
	if config.Timeout != "" {
		if d, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = d
		} else {
			fmt.Printf("Warning: Invalid timeout format for provider %s: %v. Using default %v.\n", config.ID, err, timeout)
		}
	}

	return &Adapter{
		config: config,
		client: &http.Client{Timeout: timeout}, // Long timeout for generation + polling
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return pn }

// Request structures
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
	prompt, inputImages, err := a.extractPromptAndImages(req)
	if err != nil {
		return nil, err
	}

	genResp, err := a.submitGenerationRequest(ctx, req.Model, prompt, inputImages)
	if err != nil {
		return nil, err
	}

	finalImageURL, err := a.pollForResult(ctx, genResp.PollingURL)
	if err != nil {
		return nil, err
	}

	return a.constructResponse(req.Model, genResp.ID, finalImageURL)
}

func (a *Adapter) extractPromptAndImages(req *api.ChatRequest) (string, []string, error) {
	prompt := ""
	var inputImages []string

	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == string(api.User) {
			// Extract text from Content
			if req.Messages[i].Content.Text != "" {
				prompt = req.Messages[i].Content.Text
			}

			// Extract parts
			for _, p := range req.Messages[i].Content.Parts {
				if p.Type == "text" {
					prompt += p.Text
				} else if p.Type == "image_url" && p.ImageURL != nil {
					// Process image to get base64 data
					imgData, err := processing.ProcessImageURL(p.ImageURL.URL)
					if err == nil {
						inputImages = append(inputImages, imgData.Data)
					}
				}
			}
			break
		}
	}

	if prompt == "" {
		return "", nil, fmt.Errorf("no prompt found in messages")
	}

	return prompt, inputImages, nil
}

func (a *Adapter) submitGenerationRequest(ctx context.Context, modelID, prompt string, inputImages []string) (*GenerationResponse, error) {
	reqBodyMap := map[string]interface{}{
		"prompt":           prompt,
		"safety_tolerance": 5,
	}

	if len(inputImages) == 0 {
		reqBodyMap["width"] = 1024
		reqBodyMap["height"] = 1024
	} else {
		a.enrichRequestWithImages(modelID, inputImages, reqBodyMap)
	}

	endpoint := fmt.Sprintf("%s/%s", a.config.BaseURL, modelID)

	jsonBody, err := json.Marshal(reqBodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("x-key", a.config.APIKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("BFL API error: status %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	var genResp GenerationResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &genResp, nil
}

func (a *Adapter) enrichRequestWithImages(modelID string, inputImages []string, reqBodyMap map[string]interface{}) {
	switch modelID {
	case "flux-pro-1.0-fill", "flux-fill-pro":
		// Inpainting requires "image" and optionally "mask"
		reqBodyMap["image"] = inputImages[0]
		if len(inputImages) > 1 {
			reqBodyMap["mask"] = inputImages[1]
		}

	case "flux-kontext-max", "flux-kontext-pro", "flux-2-pro", "flux-2-flex", "flux-2-klein-4b", "flux-2-klein-9b", "flux-2-max", "flux-2-dev":
		reqBodyMap["input_image"] = inputImages[0]
		for idx, img := range inputImages[1:] {
			key := fmt.Sprintf("input_image_%d", idx+2)
			reqBodyMap[key] = img
		}

	case "flux-pro-1.1", "flux-pro-1.1-ultra", "flux-pro-1.1-raw", "flux-pro-1.0", "flux-1.1-pro":
		// Standard models usually take "image_prompt" for variation/redux
		reqBodyMap["image_prompt"] = inputImages[0]

	default:
		// Fallback try input_image as it's becoming standard for BFL editing
		reqBodyMap["input_image"] = inputImages[0]
	}
}

func (a *Adapter) pollForResult(ctx context.Context, pollingURL string) (string, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Safety timeout of 10 minutes to prevent infinite polling
	// We rely on context cancellation mainly, but this is a failsafe
	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout.C:
			return "", fmt.Errorf("polling timed out after 10 minutes")
		case <-ticker.C:
			res, err := a.checkPollStatus(ctx, pollingURL)
			if err != nil {
				return "", err
			}
			if res != "" {
				return res, nil
			}
		}
	}
}

func (a *Adapter) checkPollStatus(ctx context.Context, pollingURL string) (string, error) {
	pollReq, err := http.NewRequestWithContext(ctx, "GET", pollingURL, nil)
	if err != nil {
		return "", err
	}
	pollReq.Header.Set("accept", "application/json")
	pollReq.Header.Set("x-key", a.config.APIKey)

	pollResp, err := a.client.Do(pollReq)
	if err != nil {
		return "", fmt.Errorf("polling failed: %w", err)
	}

	defer func() {
		_ = pollResp.Body.Close()
	}()

	bodyBytes, _ := io.ReadAll(pollResp.Body)

	var pollResult PollingResponse
	if err := json.Unmarshal(bodyBytes, &pollResult); err != nil {
		if pollResp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("polling failed with status %d: %s", pollResp.StatusCode, string(bodyBytes))
		}
		return "", fmt.Errorf("failed to decode polling response: %w", err)
	}

	switch pollResult.Status {
	case "Ready":
		if pollResult.Result != nil {
			return pollResult.Result.Sample, nil
		}
		return "", fmt.Errorf("status is Ready but result is missing")
	case "Error", "Failed", "Request Moderated", "Content Moderated", "Task not found":
		errMsg := pollResult.Message
		if errMsg == "" {
			errMsg = pollResult.Status
		} else {
			errMsg = fmt.Sprintf("%s (%s)", errMsg, pollResult.Status)
		}
		return "", fmt.Errorf("generation failed: %s", errMsg)
	}

	// Check for HTTP errors even if Status wasn't explicitly a failure state we know
	if pollResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("polling failed with status %d: %s", pollResp.StatusCode, pollResult.Message)
	}

	return "", nil // Continue polling
}

func (a *Adapter) constructResponse(modelID, id, imageURL string) (*api.ChatResponse, error) {
	// BFL URLs are ephemeral (10 min), so we fetch it now to provide a persistent result
	// and stay consistent with other providers in this app.
	imgData, err := processing.ProcessImageURL(imageURL)
	if err == nil {
		imageURL = fmt.Sprintf("data:%s;base64,%s", imgData.MediaType, imgData.Data)
	}

	return &api.ChatResponse{
		ID:      id,
		Model:   modelID,
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
								URL: imageURL,
							},
						},
					},
				},
				Images: []api.ContentPart{
					{
						Type: "image_url",
						ImageURL: &api.ImageURL{
							URL: imageURL,
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
