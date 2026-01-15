package google

import (
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
	registry.Register("google", NewAdapter)
}

type Adapter struct {
	config domain.ProviderConfig
	client *http.Client
}

func NewAdapter(config domain.ProviderConfig) (ports.ModelProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return "google" }

type GeminiPart struct {
	Text string `json:"text,omitempty"`
}
type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}
type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}
type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
}

func (a *Adapter) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	gr := GeminiRequest{}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		gr.Contents = append(gr.Contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: m.Content.Text}},
		})
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", 
		strings.TrimRight(a.config.BaseURL, "/"), 
		req.Model, 
		a.config.APIKey,
	)

	var gResp GeminiResponse
	if err := utils.SendRequest(ctx, a.client, "POST", url, nil, gr, &gResp); err != nil {
		return nil, err
	}

	if len(gResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates from gemini")
	}

	text := ""
	if len(gResp.Candidates[0].Content.Parts) > 0 {
		text = gResp.Candidates[0].Content.Parts[0].Text
	}

	return &schema.ChatResponse{
		ID:      fmt.Sprintf("gemini-%d", time.Now().Unix()),
		Model:   req.Model,
		Choices: []schema.Choice{{
			Index: 0,
			Message: &schema.ChatMessage{
				Role:    "assistant",
				Content: schema.Content{Text: text},
			},
			FinishReason: "stop",
		}},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan ports.StreamResult, error) {
	ch := make(chan ports.StreamResult)

	gr := GeminiRequest{}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		gr.Contents = append(gr.Contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: m.Content.Text}},
		})
	}

	// Use streamGenerateContent?alt=sse for Server-Sent Events if supported, 
	// but standard REST stream sends a JSON array. 
	// The easiest "REST" way without the SDK is to just read the chunks.
	// NOTE: Google's REST API returns a JSON array: "[{...},\n{...}]".
	// Parsing this manually is tricky. 
	// However, the ?alt=sse parameter is now supported in v1beta.
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse", 
		strings.TrimRight(a.config.BaseURL, "/"), 
		req.Model, 
		a.config.APIKey,
	)

	go func() {
		defer close(ch)
		
		// Google doesn't need extra headers for this request, but strict checking is good.
		headers := map[string]string{}

		err := utils.StreamRequest(ctx, a.client, "POST", url, headers, gr, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}
			data := strings.TrimPrefix(line, "data: ")
			
			var gResp GeminiResponse
			if err := json.Unmarshal([]byte(data), &gResp); err != nil {
				return nil
			}

			if len(gResp.Candidates) > 0 && len(gResp.Candidates[0].Content.Parts) > 0 {
				text := gResp.Candidates[0].Content.Parts[0].Text
				ch <- ports.StreamResult{Response: &schema.ChatResponse{
					Choices: []schema.Choice{{
						Delta: &schema.ChatMessage{Content: schema.Content{Text: text}},
					}},
				}}
			}
			return nil
		})

		if err != nil {
			ch <- ports.StreamResult{Err: err}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]schema.Model, error) {
	url := fmt.Sprintf("%s/models?key=%s", strings.TrimRight(a.config.BaseURL, "/"), a.config.APIKey)
	var list struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}

	if err := utils.SendRequest(ctx, a.client, "GET", url, nil, nil, &list); err != nil {
		return nil, err
	}

	var models []schema.Model
	for _, m := range list.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		models = append(models, schema.Model{
			ID:       id,
			Name:     m.DisplayName,
			Provider: a.config.ID,
			OwnedBy:  "google",
		})
	}
	return models, nil
}
