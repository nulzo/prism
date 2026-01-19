package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
)

const pn string = "google"

func init() {
	llm.Register(pn, NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return pn }

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

func Shape(req []api.ChatMessage) (GeminiRequest, error) {
	gr := GeminiRequest{}
	for _, m := range req {
		role := api.User
		if m.Role == string(api.Assistant) {
			role = api.ModelAssistant
		}
		gr.Contents = append(gr.Contents, GeminiContent{
			Role:  string(role),
			Parts: []GeminiPart{{Text: m.Content.Text}},
		})
	}
	return gr, nil
}

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	var shape, _ = Shape(req.Messages)

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		strings.TrimRight(a.config.BaseURL, "/"),
		req.Model,
		a.config.APIKey,
	)

	var gResp GeminiResponse
	if err := httpclient.SendRequest(ctx, a.client, "POST", url, nil, shape, &gResp); err != nil {
		return nil, err
	}

	if len(gResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates from gemini")
	}

	text := ""
	if len(gResp.Candidates[0].Content.Parts) > 0 {
		text = gResp.Candidates[0].Content.Parts[0].Text
	}

	return &api.ChatResponse{
		ID:    fmt.Sprintf("gemini-%d", time.Now().Unix()),
		Model: req.Model,
		Choices: []api.Choice{{
			Index: 0,
			Message: &api.ChatMessage{
				Role:    string(api.Assistant),
				Content: api.Content{Text: text},
			},
			FinishReason: "stop",
		}},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	var shape, _ = Shape(req.Messages)

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse",
		strings.TrimRight(a.config.BaseURL, "/"),
		req.Model,
		a.config.APIKey,
	)

	go func() {
		defer close(ch)

		headers := map[string]string{}

		err := httpclient.StreamRequest(ctx, a.client, "POST", url, headers, shape, func(line string) error {
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
				ch <- api.StreamResult{Response: &api.ChatResponse{
					Choices: []api.Choice{{
						Delta: &api.ChatMessage{Content: api.Content{Text: text}},
					}},
				}}
			}
			return nil
		})

		if err != nil {
			ch <- api.StreamResult{Err: err}
		}
	}()

	return ch, nil
}

func (a *Adapter) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	// Google provider uses static configuration
	return a.config.StaticModels, nil
}
