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
	"github.com/nulzo/model-router-api/internal/llm/processing"
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

	timeout := 10 * time.Minute
	if config.Timeout != "" {
		if d, err := time.ParseDuration(config.Timeout); err == nil {
			timeout = d
		} else {
			fmt.Printf("Warning: Invalid timeout format for provider %s: %v. Using default %v.\n", config.ID, err, timeout)
		}
	}

	return &Adapter{
		config: config,
		client: &http.Client{Timeout: timeout},
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return pn }

type GeminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *GeminiBlob `json:"inlineData,omitempty"`
}

type GeminiBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type GeminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type GeminiGenerationConfig struct {
	ResponseModalities []string `json:"responseModalities,omitempty"`
	Temperature        float64  `json:"temperature,omitempty"`
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate   `json:"candidates"`
	UsageMetadata GeminiUsageMetadata `json:"usageMetadata"`
}

type GeminiRequest struct {
	Contents         []GeminiContent         `json:"contents"`
	SafetySettings   []GeminiSafetySetting   `json:"safetySettings,omitempty"`
	GenerationConfig *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

func Shape(req *api.ChatRequest) (GeminiRequest, error) {
	gr := GeminiRequest{
		// default to having no moderation by default
		SafetySettings: []GeminiSafetySetting{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "BLOCK_NONE"},
			{Category: "HARM_CATEGORY_CIVIC_INTEGRITY", Threshold: "BLOCK_NONE"},
		},
	}

	if len(req.Modalities) > 0 {
		gr.GenerationConfig = &GeminiGenerationConfig{}
		var mods []string
		for _, m := range req.Modalities {
			mods = append(mods, strings.ToUpper(m))
		}
		gr.GenerationConfig.ResponseModalities = mods
	}

	if req.Temperature != 0 {
		if gr.GenerationConfig == nil {
			gr.GenerationConfig = &GeminiGenerationConfig{}
		}
		gr.GenerationConfig.Temperature = req.Temperature
	}

	for _, m := range req.Messages {
		role := api.User
		if m.Role == string(api.Assistant) {
			role = api.ModelAssistant
		}

		var parts []GeminiPart

		if m.Content.Text != "" && len(m.Content.Parts) == 0 {
			parts = append(parts, GeminiPart{Text: m.Content.Text})
		}

		for _, p := range m.Content.Parts {
			if p.Type == "text" {
				parts = append(parts, GeminiPart{Text: p.Text})
			} else if p.Type == "image_url" && p.ImageURL != nil {
				imgData, err := processing.ProcessImageURL(p.ImageURL.URL)
				if err == nil {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiBlob{
							MimeType: imgData.MediaType,
							Data:     imgData.Data,
						},
					})
				}
			}
		}

		if len(parts) > 0 {
			gr.Contents = append(gr.Contents, GeminiContent{
				Role:  string(role),
				Parts: parts,
			})
		}
	}
	return gr, nil
}

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	var shape, _ = Shape(req)

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

	var sb strings.Builder
	var images []api.ContentPart

	for _, part := range gResp.Candidates[0].Content.Parts {
		if part.Text != "" {
			sb.WriteString(part.Text)
		}
		if part.InlineData != nil {
			dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
			images = append(images, api.ContentPart{
				Type: "image_url",
				ImageURL: &api.ImageURL{
					URL: dataURL,
				},
			})
		}
	}

	content, reasoning := processing.ExtractThinking(sb.String())

	return &api.ChatResponse{
		ID:    fmt.Sprintf("gemini-%d", time.Now().Unix()),
		Model: req.Model,
		Choices: []api.Choice{{
			Index: 0,
			Message: &api.ChatMessage{
				Role:      string(api.Assistant),
				Content:   api.Content{Text: content},
				Reasoning: reasoning,
				Images:    images,
			},
			FinishReason: "stop",
		}},
		Usage: &api.ResponseUsage{
			PromptTokens:     gResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: gResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      gResp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)

	var shape, _ = Shape(req)

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse",
		strings.TrimRight(a.config.BaseURL, "/"),
		req.Model,
		a.config.APIKey,
	)

	go func() {
		defer close(ch)

		headers := map[string]string{}
		parser := processing.NewStreamParser()

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
				var sb strings.Builder
				var images []api.ContentPart

				for _, part := range gResp.Candidates[0].Content.Parts {
					if part.Text != "" {
						sb.WriteString(part.Text)
					}
					if part.InlineData != nil {
						dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
						images = append(images, api.ContentPart{
							Type: "image_url",
							ImageURL: &api.ImageURL{
								URL: dataURL,
							},
						})
					}
				}

				text := sb.String()
				c, r := parser.Process(text)
				
				ch <- api.StreamResult{Response: &api.ChatResponse{
					Choices: []api.Choice{{
						Delta: &api.ChatMessage{
							Content:   api.Content{Text: c},
							Reasoning: r,
							Images:    images,
						},
					}},
				}}
			}

			// Handle usage metadata if present in stream
			if gResp.UsageMetadata.TotalTokenCount > 0 {
				ch <- api.StreamResult{Response: &api.ChatResponse{
					Choices: []api.Choice{},
					Usage: &api.ResponseUsage{
						PromptTokens:     gResp.UsageMetadata.PromptTokenCount,
						CompletionTokens: gResp.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      gResp.UsageMetadata.TotalTokenCount,
					},
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

func (a *Adapter) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/models?key=%s&pageSize=1",
		strings.TrimRight(a.config.BaseURL, "/"),
		a.config.APIKey,
	)

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
