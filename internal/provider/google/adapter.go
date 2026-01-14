package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Adapter struct {
	config provider.ProviderConfig
	client *http.Client
}

func NewAdapter(config provider.ProviderConfig) *Adapter {
	if config.BaseURL == "" {
		config.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Adapter) Name() string {
	return "google"
}

// Gemini structures
type GeminiPart struct {
	Text string `json:"text,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiRequest struct {
	Contents         []GeminiContent `json:"contents"`
	GenerationConfig *GenConfig      `json:"generationConfig,omitempty"`
}

type GenConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	TokenCount   int           `json:"tokenCount"` 
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	// 1. Unified -> Gemini
	geminiReq := GeminiRequest{
		Contents: make([]GeminiContent, 0),
		GenerationConfig: &GenConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
			TopP:            req.TopP,
		},
	}

	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		
		content := msg.Content.Text // Simplified: using Text only
		if msg.Role == "system" {
			content = "System instruction: " + content
			role = "user"
		}

		geminiReq.Contents = append(geminiReq.Contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: content}},
		})
	}

	if a.config.APIKey == "mock-google-key" {
		return a.mockResponse(req)
	}

	// 2. Execution
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", a.config.BaseURL, req.Model, a.config.APIKey)
	reqBody, _ := json.Marshal(geminiReq)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini api error %d: %s", resp.StatusCode, string(body))
	}

	var gemResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return nil, err
	}

	// 3. Gemini -> Unified
	if len(gemResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates returned from gemini")
	}

	candidate := gemResp.Candidates[0]
	text := ""
	if len(candidate.Content.Parts) > 0 {
		text = candidate.Content.Parts[0].Text
	}

	return &schema.ChatResponse{
		ID:      "gen-" + fmt.Sprintf("%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: &schema.ChatMessage{
					Role:    "assistant",
					Content: schema.Content{Text: text},
				},
				FinishReason: "stop",
			},
		},
		Usage: &schema.ResponseUsage{
			PromptTokens:     gemResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: gemResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      gemResp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (a *Adapter) mockResponse(req *schema.ChatRequest) (*schema.ChatResponse, error) {
	content := ""
	if len(req.Messages) > 0 {
		content = req.Messages[len(req.Messages)-1].Content.Text
	}
	return &schema.ChatResponse{
		ID:      "gen-mock-google-" + fmt.Sprintf("%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: &schema.ChatMessage{
					Role:    "assistant",
					Content: schema.Content{Text: fmt.Sprintf("[Google Gemini 2026] Echoing: %s", content)},
				},
				FinishReason: "stop",
			},
		},
		Usage: &schema.ResponseUsage{
			PromptTokens:     12,
			CompletionTokens: 22,
			TotalTokens:      34,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan provider.StreamResult, error) {
	ch := make(chan provider.StreamResult)

	go func() {
		defer close(ch)
		// Mock streaming implementation
		content := ""
		if len(req.Messages) > 0 {
			content = req.Messages[len(req.Messages)-1].Content.Text
		}

		responseContent := fmt.Sprintf("[Gemini Stream] Echoing: %s", content)
		tokens := []rune(responseContent)
		chunkSize := 4

		for i := 0; i < len(tokens); i += chunkSize {
			end := i + chunkSize
			if end > len(tokens) {
				end = len(tokens)
			}

			select {
			case <-ctx.Done():
				ch <- provider.StreamResult{Err: ctx.Err()}
				return
			case <-time.After(50 * time.Millisecond):
			}

			ch <- provider.StreamResult{
				Response: &schema.ChatResponse{
					ID:      "gen-mock-stream-" + fmt.Sprintf("%d", time.Now().Unix()),
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   req.Model,
					Choices: []schema.Choice{
						{
							Index: 0,
							Delta: &schema.ChatMessage{
								Content: schema.Content{Text: string(tokens[i:end])},
							},
						},
					},
				},
			}
		}

		ch <- provider.StreamResult{
			Response: &schema.ChatResponse{
				Choices: []schema.Choice{{
					Index: 0,
					Delta: &schema.ChatMessage{},
					FinishReason: "stop",
				}},
			},
		}
	}()

	return ch, nil
}