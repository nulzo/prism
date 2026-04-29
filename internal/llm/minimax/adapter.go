package minimax

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/openai"
	"github.com/nulzo/model-router-api/pkg/api"
)

const providerType = "minimax"

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
		cfg.BaseURL = "https://api.minimax.io/v1"
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

type t2aResponse struct {
	Data *struct {
		Audio  string `json:"audio"`
		Status int    `json:"status"`
	} `json:"data"`
	ExtraInfo *struct {
		AudioFormat string `json:"audio_format"`
	} `json:"extra_info,omitempty"`
	TraceID  string `json:"trace_id,omitempty"`
	BaseResp *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp,omitempty"`
}

func (a *Adapter) CreateSpeech(ctx context.Context, req *api.UpstreamSpeechRequest) (*api.SpeechResponse, error) {
	format := speechFormat(req.ResponseFormat, "mp3")
	payload := minimaxSpeechPayload(req, format)

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	endpoint := a.ttsEndpoint()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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
		return nil, api.NewError(resp.StatusCode, "Upstream Provider Error", string(respBody))
	}

	var out t2aResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode minimax speech response: %w", err)
	}
	if out.BaseResp != nil && out.BaseResp.StatusCode != 0 {
		return nil, api.ProviderError(fmt.Sprintf("minimax speech failed: %s", out.BaseResp.StatusMsg), nil)
	}
	if out.Data == nil || out.Data.Audio == "" {
		return nil, api.ProviderError("minimax speech returned no audio payload", nil)
	}

	audio, err := decodeAudio(out.Data.Audio, a.client, ctx)
	if err != nil {
		return nil, err
	}
	if out.ExtraInfo != nil && out.ExtraInfo.AudioFormat != "" {
		format = out.ExtraInfo.AudioFormat
	}

	return &api.SpeechResponse{
		Data:        audio,
		ContentType: speechContentType(format),
	}, nil
}

func (a *Adapter) StreamSpeech(ctx context.Context, req *api.UpstreamSpeechRequest, write api.SpeechStreamWriter) error {
	resp, err := a.CreateSpeech(ctx, req)
	if err != nil {
		return err
	}
	return write(resp.ContentType, resp.Data)
}

func (a *Adapter) ttsEndpoint() string {
	baseURL := strings.TrimRight(a.config.BaseURL, "/")
	if a.config.Config != nil {
		if v := strings.TrimSpace(a.config.Config["tts_base_url"]); v != "" {
			baseURL = strings.TrimRight(v, "/")
		}
	}
	endpoint := baseURL + "/t2a_v2"
	if a.config.Config == nil || strings.TrimSpace(a.config.Config["group_id"]) == "" {
		return endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	q := u.Query()
	q.Set("GroupId", a.config.Config["group_id"])
	u.RawQuery = q.Encode()
	return u.String()
}

func minimaxSpeechPayload(req *api.UpstreamSpeechRequest, format string) map[string]any {
	payload := map[string]any{
		"model":         req.Model,
		"text":          req.Input,
		"stream":        false,
		"output_format": "hex",
		"voice_setting": map[string]any{
			"voice_id": req.Voice,
			"speed":    1.0,
			"vol":      1.0,
			"pitch":    0,
		},
		"audio_setting": map[string]any{
			"sample_rate": 32000,
			"bitrate":     128000,
			"format":      format,
			"channel":     1,
		},
	}
	if req.Speed != nil {
		payload["voice_setting"].(map[string]any)["speed"] = *req.Speed
	}
	for _, key := range []string{"language_boost", "pronunciation_dict", "voice_modify", "subtitle_enable", "output_format"} {
		if v, ok := req.Provider[key]; ok {
			payload[key] = v
		}
	}
	mergeMap(payload, "voice_setting", req.Provider)
	mergeMap(payload, "audio_setting", req.Provider)
	return payload
}

func mergeMap(payload map[string]any, key string, provider map[string]any) {
	if provider == nil {
		return
	}
	overrides, ok := provider[key].(map[string]any)
	if !ok {
		return
	}
	target, ok := payload[key].(map[string]any)
	if !ok {
		payload[key] = overrides
		return
	}
	for k, v := range overrides {
		target[k] = v
	}
}

func decodeAudio(raw string, client *http.Client, ctx context.Context) ([]byte, error) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, api.NewError(resp.StatusCode, "Upstream Provider Error", string(body))
		}
		return body, nil
	}
	return hex.DecodeString(raw)
}

func speechFormat(format string, fallback string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return fallback
	}
	return format
}

func speechContentType(format string) string {
	switch speechFormat(format, "mp3") {
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
