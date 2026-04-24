package elevenlabs

import (
	"context"
	"encoding/base64"
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
	llm.Register("elevenlabs", NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(config config.ProviderConfig) (llm.Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.elevenlabs.io/v1"
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
	return "elevenlabs"
}

func (a *Adapter) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingUnsupported}
}

func (a *Adapter) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	// ElevenLabs only supports text-to-speech
	// Extract the text from the last user message
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

	// Extract voice_id from model name (e.g., elevenlabs/JBFqnCBsd6RMkjVDRZzb)
	parts := strings.Split(req.Model, "/")
	voiceID := "JBFqnCBsd6RMkjVDRZzb" // default voice
	if len(parts) > 1 {
		voiceID = parts[1]
	}

	format := "mp3_44100_128"
	if req.Audio != nil && req.Audio.Format != "" {
		format = req.Audio.Format
	}

	url := fmt.Sprintf("%s/text-to-speech/%s?output_format=%s", strings.TrimRight(a.config.BaseURL, "/"), voiceID, format)

	payload := map[string]interface{}{
		"text":     text,
		"model_id": "eleven_multilingual_v2",
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if a.config.APIKey != "" {
		httpReq.Header.Set("xi-api-key", a.config.APIKey)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elevenlabs error: %s", string(body))
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

func (a *Adapter) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	// ElevenLabs streaming is just returning the audio chunks
	// For simplicity, we'll just call the non-streaming endpoint and return it as one chunk,
	// or we can stream the response body.

	ch := make(chan api.StreamResult)

	go func() {
		defer close(ch)
		resp, err := a.Chat(ctx, req)
		if err != nil {
			ch <- api.StreamResult{Err: err}
			return
		}

		// Convert to stream format
		resp.Object = "chat.completion.chunk"
		resp.Choices[0].Delta = resp.Choices[0].Message
		resp.Choices[0].Message = nil

		ch <- api.StreamResult{Response: resp}
	}()

	return ch, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	return nil
}
