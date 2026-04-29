package qwen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/openai"
	"github.com/nulzo/model-router-api/pkg/api"
)

const providerType = "qwen"

func init() {
	llm.Register(providerType, NewAdapter)
}

type Adapter struct {
	llm.Provider
	config config.ProviderConfig
	client *http.Client
}

func NewAdapter(cfg config.ProviderConfig) (llm.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	}
	base, err := openai.NewAdapter(cfg)
	if err != nil {
		return nil, err
	}

	timeout := 10 * time.Minute
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = d
		}
	}

	return &Adapter{
		Provider: base,
		config:   cfg,
		client:   httpclient.NewRequestClient(timeout),
	}, nil
}

func (a *Adapter) Type() string { return providerType }

type speechResponse struct {
	RequestID string `json:"request_id,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Output    struct {
		FinishReason string `json:"finish_reason,omitempty"`
		Audio        struct {
			URL       string `json:"url,omitempty"`
			Data      string `json:"data,omitempty"`
			ExpiresAt int64  `json:"expires_at,omitempty"`
		} `json:"audio,omitempty"`
	} `json:"output,omitempty"`
}

func (a *Adapter) CreateSpeech(ctx context.Context, req *api.UpstreamSpeechRequest) (*api.SpeechResponse, error) {
	payload := map[string]any{
		"model": req.Model,
		"input": qwenSpeechInput(req),
	}
	if params := providerMap(req.Provider, "parameters"); len(params) > 0 {
		payload["parameters"] = params
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/services/aigc/multimodal-generation/generation", strings.TrimRight(a.ttsBaseURL(), "/"))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, upstreamProblem(resp.StatusCode, respBody, url)
	}

	var out speechResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode qwen speech response: %w", err)
	}
	if out.Code != "" {
		return nil, api.ProviderError(fmt.Sprintf("qwen speech failed: %s", out.Message), nil)
	}

	format := speechFormat(req.ResponseFormat, "wav")
	switch {
	case out.Output.Audio.Data != "":
		audio, err := base64.StdEncoding.DecodeString(stripDataURL(out.Output.Audio.Data))
		if err != nil {
			return nil, fmt.Errorf("decode qwen speech audio: %w", err)
		}
		return &api.SpeechResponse{Data: audio, ContentType: speechContentType(format)}, nil
	case out.Output.Audio.URL != "":
		audio, contentType, err := a.downloadSpeech(ctx, out.Output.Audio.URL, format)
		if err != nil {
			return nil, err
		}
		return &api.SpeechResponse{Data: audio, ContentType: contentType}, nil
	default:
		return nil, api.ProviderError("qwen speech returned no audio payload", nil)
	}
}

func (a *Adapter) StreamSpeech(ctx context.Context, req *api.UpstreamSpeechRequest, write api.SpeechStreamWriter) error {
	resp, err := a.CreateSpeech(ctx, req)
	if err != nil {
		return err
	}
	return write(resp.ContentType, resp.Data)
}

func (a *Adapter) ttsBaseURL() string {
	if a.config.Config != nil {
		if v := strings.TrimSpace(a.config.Config["tts_base_url"]); v != "" {
			return v
		}
	}
	return "https://dashscope-intl.aliyuncs.com/api/v1"
}

func qwenSpeechInput(req *api.UpstreamSpeechRequest) map[string]any {
	input := map[string]any{
		"text":  req.Input,
		"voice": req.Voice,
	}
	for _, key := range []string{"language_type", "instructions", "optimize_instructions"} {
		if v, ok := req.Provider[key]; ok {
			input[key] = v
		}
	}
	return input
}

func (a *Adapter) downloadSpeech(ctx context.Context, url string, format string) ([]byte, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", upstreamProblem(resp.StatusCode, body, url)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = speechContentType(format)
	}
	return body, contentType, nil
}

func providerMap(src map[string]any, key string) map[string]any {
	if src == nil {
		return nil
	}
	v, ok := src[key]
	if !ok {
		return nil
	}
	out, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return out
}

func stripDataURL(v string) string {
	if i := strings.Index(v, ","); strings.HasPrefix(v, "data:") && i >= 0 {
		return v[i+1:]
	}
	return v
}

func speechFormat(format string, fallback string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return fallback
	}
	return format
}

func speechContentType(format string) string {
	switch speechFormat(format, "wav") {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "flac":
		return "audio/flac"
	case "pcm", "pcm16":
		return "audio/pcm"
	default:
		return "application/octet-stream"
	}
}

func upstreamProblem(status int, body []byte, url string) error {
	var upstreamErr *httpclient.UpstreamError
	err := &httpclient.UpstreamError{StatusCode: status, Body: body, URL: url}
	if errors.As(err, &upstreamErr) {
		return api.NewError(status, "Upstream Provider Error", string(body), api.WithLog(err))
	}
	return err
}
