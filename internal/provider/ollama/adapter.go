package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nulzo/model-router-api/internal/modeldata"
	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Adapter struct {
	config provider.ProviderConfig
	client *http.Client
}

func NewAdapter(config provider.ProviderConfig) *Adapter {
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Adapter) Name() string {
	return "ollama"
}

// Ollama specific structures
type OllamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaOptions struct {
	Temperature      float64 `json:"temperature,omitempty"`
	TopP             float64 `json:"top_p,omitempty"`
	TopK             int     `json:"top_k,omitempty"`
	Seed             int     `json:"seed,omitempty"`
	NumPredict       int     `json:"num_predict,omitempty"` // Equivalent to max_tokens
	Stop             []string `json:"stop,omitempty"`
	RepeatPenalty    float64 `json:"repeat_penalty,omitempty"`
	PresencePenalty  float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64 `json:"frequency_penalty,omitempty"`
}

type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *OllamaOptions  `json:"options,omitempty"`
	Format   string          `json:"format,omitempty"` // e.g. "json"
}

type OllamaChatResponse struct {
	Model      string        `json:"model"`
	CreatedAt  time.Time     `json:"created_at"`
	Message    OllamaMessage `json:"message"`
	Done       bool          `json:"done"`
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	EvalCount          int   `json:"eval_count"`
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	ollamaReq := a.transformRequest(req)
	ollamaReq.Stream = false

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.config.BaseURL+"/api/chat", bytes.NewBuffer(reqBody))
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
		return nil, fmt.Errorf("ollama api error %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp OllamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, err
	}

	return &schema.ChatResponse{
		ID:      fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ollamaResp.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: &schema.ChatMessage{
					Role:    ollamaResp.Message.Role,
					Content: schema.Content{Text: ollamaResp.Message.Content},
				},
				FinishReason: "stop",
			},
		},
		Usage: &schema.ResponseUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan provider.StreamResult, error) {
	ollamaReq := a.transformRequest(req)
	ollamaReq.Stream = true

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.config.BaseURL+"/api/chat", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}

	ch := make(chan provider.StreamResult)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var ollamaResp OllamaChatResponse
			if err := json.Unmarshal(scanner.Bytes(), &ollamaResp); err != nil {
				ch <- provider.StreamResult{Err: err}
				return
			}

			choice := schema.Choice{
				Index: 0,
				Delta: &schema.ChatMessage{
					Role:    ollamaResp.Message.Role,
					Content: schema.Content{Text: ollamaResp.Message.Content},
				},
			}

			if ollamaResp.Done {
				choice.FinishReason = "stop"
			}

			ch <- provider.StreamResult{
				Response: &schema.ChatResponse{
					ID:      fmt.Sprintf("ollama-%d", time.Now().UnixNano()),
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   ollamaResp.Model,
					Choices: []schema.Choice{choice},
				},
			}

			if ollamaResp.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- provider.StreamResult{Err: err}
		}
	}()

	return ch, nil
}

func (a *Adapter) transformRequest(req *schema.ChatRequest) OllamaChatRequest {
	messages := make([]OllamaMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = OllamaMessage{
			Role:    m.Role,
			Content: m.Content.Text,
		}
	}

	options := &OllamaOptions{
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		TopK:             req.TopK,
		Seed:             req.Seed,
		NumPredict:       req.MaxTokens,
		RepeatPenalty:    req.RepetitionPenalty,
		PresencePenalty:  req.PresencePenalty,
		FrequencyPenalty: req.FrequencyPenalty,
	}

	if req.Stop != nil {
		options.Stop = req.Stop.Val
	}

	format := ""
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		format = "json"
	}

	return OllamaChatRequest{
		Model:    req.Model,
		Messages: messages,
		Options:  options,
		Format:   format,
	}
}

type OllamaModelDetails struct {
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

type OllamaModel struct {
	Name       string             `json:"name"`
	Model      string             `json:"model"`
	ModifiedAt time.Time          `json:"modified_at"`
	Size       int64              `json:"size"`
	Digest     string             `json:"digest"`
	Details    OllamaModelDetails `json:"details"`
}

type OllamaModelList struct {
	Models []OllamaModel `json:"models"`
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.config.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama models api error %d: %s", resp.StatusCode, string(body))
	}

	var list OllamaModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	var models []schema.Model
	for _, m := range list.Models {
		mod := schema.Model{
			ID:       m.Name,
			Created:  m.ModifiedAt.Unix(),
			Object:   "model",
			OwnedBy:  "ollama",
			Provider: "ollama",
			Architecture: schema.Architecture{
				Modality: "text->text",
			},
		}

		// Enrich from KnownModels if available
		if info, ok := modeldata.KnownModels[m.Name]; ok {
			mod.Name = info.Name
			mod.Description = info.Description
			mod.ContextLength = info.ContextLength
			mod.Architecture = info.Architecture
			mod.Pricing = info.Pricing
			mod.TopProvider = info.TopProvider
		} else {
			mod.Name = m.Name
			mod.Pricing = schema.Pricing{Prompt: "0", Completion: "0"} // Ollama is local/free
		}

		models = append(models, mod)
	}

	return models, nil
}