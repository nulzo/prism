package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/httpclient"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/processing"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

const pn string = "google"

func init() {
	llm.Register(pn, NewAdapter)
}

type Adapter struct {
	config config.ProviderConfig
	client *http.Client
	stream *http.Client
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
		client: httpclient.NewRequestClient(timeout),
		stream: httpclient.NewStreamingClient(),
	}, nil
}

func (a *Adapter) Name() string { return a.config.ID }
func (a *Adapter) Type() string { return pn }

type GeminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *GeminiBlob `json:"inlineData,omitempty"`
	// Thought is Gemini's marker that this part is a thinking trace rather
	// than the user-visible answer. Present only on response parts for
	// models that return thoughts (e.g. gemini-*-thinking).
	Thought bool `json:"thought,omitempty"`
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
	Content       GeminiContent        `json:"content"`
	FinishReason  string               `json:"finishReason"`
	SafetyRatings []GeminiSafetyRating `json:"safetyRatings,omitempty"`
}

type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	// ThoughtsTokenCount is Gemini's reasoning-tokens counter. Only present
	// on thinking-capable models when thinking actually ran.
	ThoughtsTokenCount      int `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

// toResponseUsage converts Gemini usage metadata into the unified
// ResponseUsage shape with reasoning/cache details populated where
// available.
func (u GeminiUsageMetadata) toResponseUsage() *api.ResponseUsage {
	if u.TotalTokenCount == 0 && u.PromptTokenCount == 0 && u.CandidatesTokenCount == 0 {
		return nil
	}
	usage := &api.ResponseUsage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: u.CandidatesTokenCount,
		TotalTokens:      u.TotalTokenCount,
	}
	if u.ThoughtsTokenCount > 0 {
		usage.CompletionTokensDetails = &api.CompletionTokensDetails{
			ReasoningTokens: u.ThoughtsTokenCount,
		}
	}
	if u.CachedContentTokenCount > 0 {
		usage.PromptTokensDetails = &api.PromptTokensDetails{
			CachedTokens: u.CachedContentTokenCount,
		}
	}
	return usage
}

type GeminiSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
}

type GeminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type GeminiGenerationConfig struct {
	ResponseModalities []string              `json:"responseModalities,omitempty"`
	Temperature        float64               `json:"temperature,omitempty"`
	ThinkingConfig     *GeminiThinkingConfig `json:"thinkingConfig,omitempty"`
	SpeechConfig       *GeminiSpeechConfig   `json:"speechConfig,omitempty"`
}

type GeminiSpeechConfig struct {
	VoiceConfig *GeminiVoiceConfig `json:"voiceConfig,omitempty"`
}

type GeminiVoiceConfig struct {
	PrebuiltVoiceConfig *GeminiPrebuiltVoiceConfig `json:"prebuiltVoiceConfig,omitempty"`
}

type GeminiPrebuiltVoiceConfig struct {
	VoiceName string `json:"voiceName,omitempty"`
}

// GeminiThinkingConfig controls reasoning output for Gemini thinking models.
// See https://ai.google.dev/gemini-api/docs/thinking. ThinkingBudget=0 turns
// thinking off; omitting it defers to the model default. IncludeThoughts=true
// asks Gemini to emit `thought: true` parts so we can surface them as
// reasoning tokens.
type GeminiThinkingConfig struct {
	ThinkingBudget  *int `json:"thinkingBudget,omitempty"`
	IncludeThoughts bool `json:"includeThoughts,omitempty"`
}

type GeminiResponse struct {
	Candidates     []GeminiCandidate     `json:"candidates"`
	UsageMetadata  GeminiUsageMetadata   `json:"usageMetadata"`
	PromptFeedback *GeminiPromptFeedback `json:"promptFeedback,omitempty"`
}

type GeminiPromptFeedback struct {
	BlockReason   string               `json:"blockReason,omitempty"`
	SafetyRatings []GeminiSafetyRating `json:"safetyRatings,omitempty"`
}

type GeminiUpstreamErrorResponse struct {
	Error struct {
		Code    int         `json:"code"`
		Message string      `json:"message"`
		Status  string      `json:"status"`
		Details interface{} `json:"details"`
	} `json:"error"`
}

type GeminiRequest struct {
	Contents         []GeminiContent         `json:"contents"`
	SafetySettings   []GeminiSafetySetting   `json:"safetySettings,omitempty"`
	GenerationConfig *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

type GeminiOpenAICompatPayload struct {
	*api.UpstreamChatRequest
	ExtraBody GeminiOpenAIExtraBody `json:"extra_body,omitempty"`
}

type GeminiOpenAIExtraBody struct {
	Google GeminiOpenAIExtraBodyGoogle `json:"google,omitempty"`
}

type GeminiOpenAIExtraBodyGoogle struct {
	SafetySettings []GeminiSafetySetting `json:"safety_settings,omitempty"`
}

func usesOpenAICompat(req *api.UpstreamChatRequest) bool {
	if len(req.Tools) > 0 || req.ToolChoice != nil {
		return true
	}

	for _, msg := range req.Messages {
		if msg.Role == "tool" || len(msg.ToolCalls) > 0 || msg.ToolCallID != "" {
			return true
		}
	}

	return false
}

func defaultSafetySettings() []GeminiSafetySetting {
	return []GeminiSafetySetting{
		{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "OFF"},
		{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "OFF"},
	}
}

func geminiSpeechConfig(voice string) *GeminiSpeechConfig {
	if strings.TrimSpace(voice) == "" {
		voice = "Kore"
	}
	return &GeminiSpeechConfig{
		VoiceConfig: &GeminiVoiceConfig{
			PrebuiltVoiceConfig: &GeminiPrebuiltVoiceConfig{
				VoiceName: voice,
			},
		},
	}
}

func Shape(req *api.UpstreamChatRequest) (GeminiRequest, error) {
	gr := GeminiRequest{
		// Use the least restrictive documented setting for all configurable
		// Gemini safety categories. Some provider-enforced core harms may still
		// be blocked upstream and cannot be bypassed by the gateway.
		SafetySettings: defaultSafetySettings(),
	}

	if len(req.Modalities) > 0 {
		gr.GenerationConfig = &GeminiGenerationConfig{}
		var mods []string
		for _, m := range req.Modalities {
			mods = append(mods, strings.ToUpper(m))
		}
		gr.GenerationConfig.ResponseModalities = mods
	}

	if req.Audio != nil && req.Audio.Voice != "" {
		if gr.GenerationConfig == nil {
			gr.GenerationConfig = &GeminiGenerationConfig{}
		}
		gr.GenerationConfig.SpeechConfig = geminiSpeechConfig(req.Audio.Voice)
	}

	if req.Temperature != 0 {
		if gr.GenerationConfig == nil {
			gr.GenerationConfig = &GeminiGenerationConfig{}
		}
		gr.GenerationConfig.Temperature = req.Temperature
	}

	// Translate the router's ReasoningConfig into Gemini's thinkingConfig.
	// Only Gemini "-thinking" models honor this; for non-thinking models
	// the extra field is ignored upstream so we always emit it when the
	// caller asked for reasoning.
	if req.Reasoning != nil && (req.Reasoning.IsEnabled() || req.Reasoning.Exclude) {
		if gr.GenerationConfig == nil {
			gr.GenerationConfig = &GeminiGenerationConfig{}
		}
		cfg := &GeminiThinkingConfig{IncludeThoughts: !req.Reasoning.Exclude}
		switch {
		case req.Reasoning.MaxTokens > 0:
			b := req.Reasoning.MaxTokens
			cfg.ThinkingBudget = &b
		case req.Reasoning.Effort != "":
			// Gemini exposes a numeric budget, so map effort buckets to a
			// conservative token count. Numbers chosen to roughly mirror
			// OpenRouter's effort percentages applied to a 24k budget.
			if budget := effortToThinkingBudget(req.Reasoning.Effort); budget != nil {
				cfg.ThinkingBudget = budget
			}
		}
		gr.GenerationConfig.ThinkingConfig = cfg
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

func (a *Adapter) handleUpstreamError(err error) error {
	var upstreamErr *httpclient.UpstreamError
	if !errors.As(err, &upstreamErr) {
		return err
	}

	var apiErr GeminiUpstreamErrorResponse
	if jsonErr := json.Unmarshal(upstreamErr.Body, &apiErr); jsonErr != nil || apiErr.Error.Message == "" {
		return api.NewError(
			upstreamErr.StatusCode,
			"Upstream Provider Error",
			string(upstreamErr.Body),
			api.WithLog(err),
		)
	}

	opts := []api.ProblemOption{
		api.WithType("about:blank"),
		api.WithLog(err),
		api.WithExtension("upstream_status", apiErr.Error.Status),
	}
	if apiErr.Error.Details != nil {
		opts = append(opts, api.WithExtension("upstream_details", apiErr.Error.Details))
	}

	return api.NewError(
		upstreamErr.StatusCode,
		"Upstream Provider Error",
		apiErr.Error.Message,
		opts...,
	)
}

func geminiResponseError(resp *GeminiResponse) error {
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return api.NewError(
			http.StatusBadRequest,
			"Upstream Provider Error",
			fmt.Sprintf("Gemini blocked the prompt: %s", resp.PromptFeedback.BlockReason),
			api.WithType("about:blank"),
			api.WithExtension("block_reason", resp.PromptFeedback.BlockReason),
			api.WithExtension("safety_ratings", resp.PromptFeedback.SafetyRatings),
		)
	}

	if len(resp.Candidates) == 0 {
		return api.NewError(
			http.StatusBadGateway,
			"Upstream Provider Error",
			"Gemini returned no candidates",
			api.WithType("about:blank"),
		)
	}

	candidate := resp.Candidates[0]
	if candidate.FinishReason == "SAFETY" {
		return api.NewError(
			http.StatusBadRequest,
			"Upstream Provider Error",
			"Gemini blocked the generated response for safety reasons",
			api.WithType("about:blank"),
			api.WithExtension("finish_reason", candidate.FinishReason),
			api.WithExtension("safety_ratings", candidate.SafetyRatings),
		)
	}

	if len(candidate.Content.Parts) == 0 {
		return api.NewError(
			http.StatusBadGateway,
			"Upstream Provider Error",
			"Gemini returned an empty candidate",
			api.WithType("about:blank"),
			api.WithExtension("finish_reason", candidate.FinishReason),
			api.WithExtension("safety_ratings", candidate.SafetyRatings),
		)
	}

	return nil
}

// extractResponseParts splits Gemini response parts into the four wire
// projections we care about: visible text, reasoning text (parts with
// `thought: true`), images, and audio. Thought and content parts are kept
// in their original order so streaming consumers can interleave them
// correctly.
func extractResponseParts(parts []GeminiPart) (content string, reasoning string, images []api.ContentPart, audio *api.AudioOutput) {
	var textSB, thoughtSB strings.Builder

	for _, part := range parts {
		if part.Text != "" {
			if part.Thought {
				thoughtSB.WriteString(part.Text)
			} else {
				textSB.WriteString(part.Text)
			}
		}
		if part.InlineData == nil {
			continue
		}

		dataURL := fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
		if strings.HasPrefix(part.InlineData.MimeType, "audio/") {
			audio = &api.AudioOutput{
				Data:     part.InlineData.Data,
				Format:   "pcm",
				MimeType: "audio/pcm",
			}
			continue
		}

		images = append(images, api.ContentPart{
			Type:     "image_url",
			ImageURL: &api.ImageURL{URL: dataURL},
		})
	}

	return textSB.String(), thoughtSB.String(), images, audio
}

// effortToThinkingBudget maps OpenRouter-style effort buckets to Gemini
// `thinkingBudget` token counts. Defaults calibrated so `high`/`xhigh`
// unlock the full adaptive budget (-1 signals "auto" to Gemini), while
// `none` turns thinking off entirely. Unknown inputs return nil so the
// provider default applies.
// stripReasoningField returns a copy of the request with the router-only
// `Reasoning` field cleared. Providers proxied via the OpenAI-compat shim
// reject unknown fields, so we must hide the normalized control before
// forwarding. Reasoning is still propagated to the caller because the
// response stream carries `reasoning_content` deltas that
// ChatMessage.UnmarshalJSON collapses into our `Reasoning` field.
func stripReasoningField(req *api.UpstreamChatRequest) *api.UpstreamChatRequest {
	if req == nil || req.Reasoning == nil {
		return req
	}
	cp := *req
	cp.Reasoning = nil
	return &cp
}

func openAICompatPayload(req *api.UpstreamChatRequest) any {
	stripped := stripReasoningField(req)
	if stripped == nil {
		return nil
	}
	return &GeminiOpenAICompatPayload{
		UpstreamChatRequest: stripped,
		ExtraBody: GeminiOpenAIExtraBody{
			Google: GeminiOpenAIExtraBodyGoogle{
				SafetySettings: defaultSafetySettings(),
			},
		},
	}
}

func effortToThinkingBudget(effort string) *int {
	var v int
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "xhigh", "high":
		v = -1
		return &v
	case "medium":
		v = 4096
		return &v
	case "low":
		v = 1024
		return &v
	case "minimal":
		v = 256
		return &v
	case "none":
		v = 0
		return &v
	}
	return nil
}

func (a *Adapter) Capabilities() llm.Capabilities {
	return llm.Capabilities{ToolCalling: llm.ToolCallingOpenAICompat}
}

func (a *Adapter) CreateSpeech(ctx context.Context, req *api.UpstreamSpeechRequest) (*api.SpeechResponse, error) {
	format := strings.ToLower(strings.TrimSpace(req.ResponseFormat))
	if format != "" && format != "pcm" && format != "pcm16" {
		return nil, api.BadRequestError("google tts currently supports response_format 'pcm'")
	}

	shape := GeminiRequest{
		Contents: []GeminiContent{{
			Parts: []GeminiPart{{Text: req.Input}},
		}},
		SafetySettings: defaultSafetySettings(),
		GenerationConfig: &GeminiGenerationConfig{
			ResponseModalities: []string{"AUDIO"},
			SpeechConfig:       geminiSpeechConfig(req.Voice),
		},
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		strings.TrimRight(a.config.BaseURL, "/"),
		req.Model,
		a.config.APIKey,
	)

	var gResp GeminiResponse
	if err := httpclient.SendRequest(ctx, a.client, "POST", url, nil, shape, &gResp); err != nil {
		return nil, a.handleUpstreamError(err)
	}
	if err := geminiResponseError(&gResp); err != nil {
		return nil, err
	}

	for _, part := range gResp.Candidates[0].Content.Parts {
		if part.InlineData == nil || !strings.HasPrefix(part.InlineData.MimeType, "audio/") {
			continue
		}
		audio, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
		if err != nil {
			return nil, err
		}
		return &api.SpeechResponse{
			Data:        audio,
			ContentType: "audio/pcm",
		}, nil
	}
	return nil, api.ProviderError("google tts returned no audio payload", nil)
}

func (a *Adapter) StreamSpeech(ctx context.Context, req *api.UpstreamSpeechRequest, write api.SpeechStreamWriter) error {
	resp, err := a.CreateSpeech(ctx, req)
	if err != nil {
		return err
	}
	return write(resp.ContentType, resp.Data)
}

func (a *Adapter) Chat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	if usesOpenAICompat(req) {
		logger.Debug("using Gemini OpenAI-compatible chat completions for tool-enabled request",
			zap.String("provider", a.config.ID),
			zap.String("model", req.Model),
			zap.Int("tools", len(req.Tools)),
		)
		return a.chatOpenAICompat(ctx, req)
	}

	shape, _ := Shape(req)

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		strings.TrimRight(a.config.BaseURL, "/"),
		req.Model,
		a.config.APIKey,
	)

	var gResp GeminiResponse
	if err := httpclient.SendRequest(ctx, a.client, "POST", url, nil, shape, &gResp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	if err := geminiResponseError(&gResp); err != nil {
		return nil, err
	}

	text, thoughtText, images, audio := extractResponseParts(gResp.Candidates[0].Content.Parts)

	// Prefer the explicit `thought: true` parts when the thinking-aware
	// model returned them; fall back to the legacy <think> tag heuristic
	// only if we didn't get any native thinking content.
	reasoning := thoughtText
	content := text
	if reasoning == "" {
		content, reasoning = processing.ExtractThinking(text)
	}

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
				Audio:     audio,
			},
			FinishReason:       strings.ToLower(gResp.Candidates[0].FinishReason),
			NativeFinishReason: gResp.Candidates[0].FinishReason,
		}},
		Usage: gResp.UsageMetadata.toResponseUsage(),
	}, nil
}

func (a *Adapter) chatOpenAICompat(ctx context.Context, req *api.UpstreamChatRequest) (*api.ChatResponse, error) {
	var resp api.ChatResponse

	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	url := fmt.Sprintf("%s/openai/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))
	req.Stream = false

	// Gemini's OpenAI-compat shim rejects unknown fields. Strip the
	// router-only reasoning object; when it's set, the tool-enabled path
	// below will surface reasoning via the provider's reasoning_content /
	// reasoning deltas (already aliased by ChatMessage.UnmarshalJSON).
	payload := openAICompatPayload(req)

	if err := httpclient.SendRequest(ctx, a.client, "POST", url, headers, payload, &resp); err != nil {
		return nil, a.handleUpstreamError(err)
	}

	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if choice.Message != nil {
			content, reasoning := processing.ExtractThinking(choice.Message.Content.Text)
			choice.Message.Content.Text = content
			choice.Message.Reasoning = reasoning
		}
	}

	return &resp, nil
}

func (a *Adapter) Stream(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	if usesOpenAICompat(req) {
		logger.Debug("using Gemini OpenAI-compatible stream chat completions for tool-enabled request",
			zap.String("provider", a.config.ID),
			zap.String("model", req.Model),
			zap.Int("tools", len(req.Tools)),
		)
		return a.streamOpenAICompat(ctx, req)
	}

	ch := make(chan api.StreamResult)

	shape, _ := Shape(req)

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse",
		strings.TrimRight(a.config.BaseURL, "/"),
		req.Model,
		a.config.APIKey,
	)

	go func() {
		defer close(ch)

		headers := map[string]string{}
		parser := processing.NewStreamParser()

		err := httpclient.StreamRequest(ctx, a.stream, "POST", url, headers, shape, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}
			data := strings.TrimPrefix(line, "data: ")

			var gResp GeminiResponse
			if err := json.Unmarshal([]byte(data), &gResp); err != nil {
				return nil
			}

			if len(gResp.Candidates) == 0 {
				if err := geminiResponseError(&gResp); err != nil {
					return err
				}
				return nil
			}

			if len(gResp.Candidates[0].Content.Parts) > 0 {
				text, thoughtText, images, audio := extractResponseParts(gResp.Candidates[0].Content.Parts)
				// When the model signals reasoning explicitly, forward it
				// as-is and bypass the <think> tag scanner; otherwise let
				// the parser handle legacy tag-based reasoning.
				var c, r string
				if thoughtText != "" {
					c = text
					r = thoughtText
				} else {
					c, r = parser.Process(text)
				}

				ch <- api.StreamResult{Response: &api.ChatResponse{
					Choices: []api.Choice{{
						Delta: &api.ChatMessage{
							Content:   api.Content{Text: c},
							Reasoning: r,
							Images:    images,
							Audio:     audio,
						},
					}},
				}}
			}

			// Handle usage metadata if present in stream
			if usage := gResp.UsageMetadata.toResponseUsage(); usage != nil {
				ch <- api.StreamResult{Response: &api.ChatResponse{
					Choices: []api.Choice{},
					Usage:   usage,
				}}
			}
			return nil
		})
		if err != nil {
			ch <- api.StreamResult{Err: a.handleUpstreamError(err)}
		}
	}()

	return ch, nil
}

func (a *Adapter) streamOpenAICompat(ctx context.Context, req *api.UpstreamChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)
	req.Stream = true
	req.StreamOptions = &api.StreamOptions{IncludeUsage: true}

	url := fmt.Sprintf("%s/openai/chat/completions", strings.TrimRight(a.config.BaseURL, "/"))
	headers := map[string]string{
		"Authorization": "Bearer " + a.config.APIKey,
	}

	payload := openAICompatPayload(req)

	go func() {
		defer close(ch)

		parsers := make(map[int]*processing.StreamParser)

		err := httpclient.StreamRequest(ctx, a.stream, "POST", url, headers, payload, func(line string) error {
			if !strings.HasPrefix(line, "data: ") {
				return nil
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil
			}

			var chatResp api.ChatResponse
			if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
				return nil
			}

			for i := range chatResp.Choices {
				choice := &chatResp.Choices[i]
				if choice.Delta == nil {
					continue
				}
				// Trust a native reasoning delta when the provider sends
				// one (reasoning_content is folded into Reasoning by the
				// unmarshaller) and skip the tag-scanner.
				if choice.Delta.Reasoning != "" {
					continue
				}
				idx := choice.Index
				parser, ok := parsers[idx]
				if !ok {
					parser = processing.NewStreamParser()
					parsers[idx] = parser
				}
				c, r := parser.Process(choice.Delta.Content.Text)
				choice.Delta.Content.Text = c
				if r != "" {
					choice.Delta.Reasoning = r
				}
			}

			ch <- api.StreamResult{Response: &chatResp}
			return nil
		})
		if err != nil {
			ch <- api.StreamResult{Err: a.handleUpstreamError(err)}
		}
	}()

	return ch, nil
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
