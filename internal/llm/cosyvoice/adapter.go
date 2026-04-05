package cosyvoice

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
)

func init() {
	llm.Register("cosyvoice", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:50000"
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
	return "cosyvoice"
}

func (a *Adapter) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	var text string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == string(api.User) {
			text = req.Messages[i].Content.Text
			break
		}
	}

	if text == "" {
		return nil, fmt.Errorf("no user message found to convert to speech")
	}

	// Extract speaker ID from model name (e.g., cosyvoice/中文女)
	parts := strings.Split(req.Model, "/")
	spkID := "中文女" // default speaker
	if len(parts) > 1 {
		spkID = parts[1]
	}

	url := fmt.Sprintf("%s/inference_sft", strings.TrimRight(a.config.BaseURL, "/"))

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	if err := w.WriteField("tts_text", text); err != nil {
		return nil, err
	}
	if err := w.WriteField("spk_id", spkID); err != nil {
		return nil, err
	}
	_ = w.Close()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, &b)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cosyvoice error: %s", string(body))
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	base64Audio := base64.StdEncoding.EncodeToString(audioBytes)

	chatResp := &api.ChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Created: time.Now().Unix(),
		Model:   req.Model,
		Object:  "chat.completion",
		Choices: []api.Choice{
			{
				Index: 0,
				Message: &api.ChatMessage{
					Role: string(api.Assistant),
					Audio: &api.AudioOutput{
						ID:         fmt.Sprintf("audio-%d", time.Now().UnixNano()),
						Data:       base64Audio,
						Transcript: text,
					},
				},
				FinishReason: "stop",
			},
		},
	}

	return chatResp, nil
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

		resp.Object = "chat.completion.chunk"
		resp.Choices[0].Delta = resp.Choices[0].Message
		resp.Choices[0].Message = nil

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
