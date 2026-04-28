package api

import "encoding/json"

type ChatRequest struct {
	// message array is required, dive in and deep validate
	Messages []ChatMessage `json:"messages" binding:"required,min=1,dive"`

	// the model to send request to, generally in shape `<provider>/<model>`
	Model string `json:"model" binding:"required"`

	// Allows to force the model to produce specific output format.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// Can be string or []string
	Stop *Stop `json:"stop,omitempty"`

	// Enable streaming, defaults to `false` (empty)
	Stream bool `json:"stream,omitempty"`

	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// LLM Parameters
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         float64         `json:"temperature,omitempty"`
	TopP                float64         `json:"top_p,omitempty"`
	TopK                int             `json:"top_k,omitempty"`
	FrequencyPenalty    float64         `json:"frequency_penalty,omitempty"`
	PresencePenalty     float64         `json:"presence_penalty,omitempty"`
	RepetitionPenalty   float64         `json:"repetition_penalty,omitempty"`
	Seed                int             `json:"seed,omitempty"`
	LogitBias           map[int]float64 `json:"logit_bias,omitempty"`
	TopLogprobs         int             `json:"top_logprobs,omitempty"`
	MinP                float64         `json:"min_p,omitempty"`
	TopA                float64         `json:"top_a,omitempty"`

	// Tool calling
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice interface{} `json:"tool_choice,omitempty"` // "none", "auto", or object

	// Advanced optional parameters
	Prediction *Prediction `json:"prediction,omitempty"`

	// Plugins and Extensions
	Plugins    []PluginConfig    `json:"plugins,omitempty"`
	Extensions []ExtensionConfig `json:"extensions,omitempty"`

	// OpenRouter-only parameters
	Transforms []string             `json:"transforms,omitempty"`
	Models     []string             `json:"models,omitempty"`
	Route      string               `json:"route,omitempty"` // 'fallback'
	Provider   *ProviderPreferences `json:"provider,omitempty"`
	User       string               `json:"user,omitempty"`
	Modalities []string             `json:"modalities,omitempty"`
	Audio      *AudioConfig         `json:"audio,omitempty"`

	// Debug options
	Debug *DebugOptions `json:"debug,omitempty"`

	// Reasoning / thinking tokens (OpenRouter-aligned, see
	// https://openrouter.ai/docs/use-cases/reasoning-tokens). One of
	// Effort or MaxTokens may be set; if both are unset, Enabled defaults
	// from the presence of either field. Providers that cannot honor the
	// exact requested shape (e.g. Anthropic only accepts MaxTokens, OpenAI
	// only accepts Effort) translate across in their adapters.
	Reasoning *ReasoningConfig `json:"reasoning,omitempty"`

	// IncludeReasoning is the legacy OpenRouter parameter that predates the
	// `reasoning` object. `true` is equivalent to `reasoning: {}`;
	// `false` is equivalent to `reasoning: { "exclude": true }`. Ignored
	// when Reasoning is already set so the newer field always wins.
	IncludeReasoning *bool `json:"include_reasoning,omitempty"`
}

// UpstreamChatRequest is the provider-safe request shape that can be forwarded
// to upstream model APIs. It intentionally excludes router-only fields such as
// plugins, extensions, transforms, fallback routing, and debug flags.
type UpstreamChatRequest struct {
	Messages []ChatMessage `json:"messages"`
	Model    string        `json:"model"`

	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Stop           *Stop           `json:"stop,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	StreamOptions  *StreamOptions  `json:"stream_options,omitempty"`

	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         float64         `json:"temperature,omitempty"`
	TopP                float64         `json:"top_p,omitempty"`
	TopK                int             `json:"top_k,omitempty"`
	FrequencyPenalty    float64         `json:"frequency_penalty,omitempty"`
	PresencePenalty     float64         `json:"presence_penalty,omitempty"`
	RepetitionPenalty   float64         `json:"repetition_penalty,omitempty"`
	Seed                int             `json:"seed,omitempty"`
	LogitBias           map[int]float64 `json:"logit_bias,omitempty"`
	TopLogprobs         int             `json:"top_logprobs,omitempty"`
	MinP                float64         `json:"min_p,omitempty"`
	TopA                float64         `json:"top_a,omitempty"`

	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice interface{} `json:"tool_choice,omitempty"`

	Prediction *Prediction  `json:"prediction,omitempty"`
	Modalities []string     `json:"modalities,omitempty"`
	Audio      *AudioConfig `json:"audio,omitempty"`

	// Reasoning is the normalized reasoning configuration forwarded to
	// provider adapters. The adapter is responsible for translating this
	// into its native shape (reasoning_effort, thinking_config,
	// thinking.budget_tokens, etc.) before it hits the upstream API.
	Reasoning *ReasoningConfig `json:"reasoning,omitempty"`
}

func (r *ChatRequest) ToUpstream() *UpstreamChatRequest {
	if r == nil {
		return nil
	}

	upstream := &UpstreamChatRequest{
		Messages:            append([]ChatMessage(nil), r.Messages...),
		Model:               r.Model,
		ResponseFormat:      r.ResponseFormat,
		Stop:                r.Stop,
		Stream:              r.Stream,
		StreamOptions:       r.StreamOptions,
		MaxTokens:           r.MaxTokens,
		MaxCompletionTokens: r.MaxCompletionTokens,
		Temperature:         r.Temperature,
		TopP:                r.TopP,
		TopK:                r.TopK,
		FrequencyPenalty:    r.FrequencyPenalty,
		PresencePenalty:     r.PresencePenalty,
		RepetitionPenalty:   r.RepetitionPenalty,
		Seed:                r.Seed,
		TopLogprobs:         r.TopLogprobs,
		MinP:                r.MinP,
		TopA:                r.TopA,
		Tools:               append([]Tool(nil), r.Tools...),
		ToolChoice:          r.ToolChoice,
		Prediction:          r.Prediction,
		Modalities:          append([]string(nil), r.Modalities...),
		Audio:               r.Audio,
		Reasoning:           r.Reasoning.Normalize(r.IncludeReasoning),
	}

	if r.LogitBias != nil {
		upstream.LogitBias = make(map[int]float64, len(r.LogitBias))
		for k, v := range r.LogitBias {
			upstream.LogitBias[k] = v
		}
	}

	return upstream
}

type AudioConfig struct {
	Voice  string `json:"voice,omitempty"`
	Format string `json:"format,omitempty"`
}

type SpeechRequest struct {
	Input          string                 `json:"input" binding:"required"`
	Model          string                 `json:"model" binding:"required"`
	Voice          string                 `json:"voice" binding:"required"`
	ResponseFormat string                 `json:"response_format,omitempty"`
	Speed          *float64               `json:"speed,omitempty"`
	Provider       map[string]interface{} `json:"provider,omitempty"`
}

type UpstreamSpeechRequest struct {
	Input          string                 `json:"input"`
	Model          string                 `json:"model"`
	Voice          string                 `json:"voice"`
	ResponseFormat string                 `json:"response_format,omitempty"`
	Speed          *float64               `json:"speed,omitempty"`
	Provider       map[string]interface{} `json:"provider,omitempty"`
}

type ChatMessage struct {
	Role string `json:"role" binding:"required,oneof=user assistant system tool"`
	// Content handles the union string | []ContentPart.
	Content Content `json:"content"`
	// Reasoning is the normalized "thinking" text. Providers expose this
	// under a few different names; the UnmarshalJSON hook collapses them:
	//   * OpenRouter / Anthropic / default: `reasoning`
	//   * DeepSeek and most OpenAI-compat providers: `reasoning_content`
	// We always emit `reasoning` on the wire for consistency.
	Reasoning string `json:"reasoning,omitempty"`
	// ReasoningDetails is the typed, provider-preserving view of reasoning
	// (OpenRouter `reasoning_details` array). Each entry is one of
	// reasoning.text / reasoning.summary / reasoning.encrypted. This must be
	// passed back verbatim in the next request's assistant message for
	// providers (notably Anthropic) that require the reasoning block chain
	// to remain intact across a tool-use round-trip.
	ReasoningDetails []ReasoningDetail `json:"reasoning_details,omitempty"`
	Name             string            `json:"name,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	Images           []ContentPart     `json:"images,omitempty"`
	Audio            *AudioOutput      `json:"audio,omitempty"`
	Annotations      []interface{}     `json:"annotations,omitempty"`
}

// chatMessageAlias is a shadow type used to break json.Unmarshal recursion so
// ChatMessage.UnmarshalJSON can add post-processing (reasoning_content alias
// collapse) without infinite loop.
type chatMessageAlias ChatMessage

// UnmarshalJSON folds the OpenAI-compat `reasoning_content` field into
// `Reasoning` so downstream code only has to look in one place. If both are
// present, `reasoning` wins (OpenRouter's canonical field).
func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	var wire struct {
		chatMessageAlias
		ReasoningContent string `json:"reasoning_content,omitempty"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*m = ChatMessage(wire.chatMessageAlias)
	if m.Reasoning == "" && wire.ReasoningContent != "" {
		m.Reasoning = wire.ReasoningContent
	}
	return nil
}

// ReasoningConfig is the OpenRouter-style reasoning control. Exactly one of
// Effort or MaxTokens should be set; adapters translate whichever is set
// into their native control. Exclude=true means the provider should still
// reason but the result must not appear in the response body.
type ReasoningConfig struct {
	// Effort is one of "xhigh", "high", "medium", "low", "minimal", "none".
	// Intended for OpenAI-style providers (o-series, GPT-5, Grok).
	Effort string `json:"effort,omitempty"`
	// MaxTokens is a direct token budget for reasoning. Intended for
	// Anthropic and Gemini thinking models.
	MaxTokens int `json:"max_tokens,omitempty"`
	// Exclude hides reasoning tokens from the response while still allowing
	// the provider to reason internally (where supported).
	Exclude bool `json:"exclude,omitempty"`
	// Enabled lets callers opt-in to reasoning at a default level without
	// specifying effort or max_tokens. Providers treat `enabled: true` as
	// medium effort.
	Enabled *bool `json:"enabled,omitempty"`
}

// Normalize returns a concrete ReasoningConfig after merging in the legacy
// `include_reasoning` boolean. Returns nil when neither the struct nor the
// legacy flag indicates any reasoning request, so the upstream layer only
// has to check for a non-nil config.
func (r *ReasoningConfig) Normalize(legacyInclude *bool) *ReasoningConfig {
	if r != nil {
		// Copy so callers can't mutate the original after ToUpstream.
		out := *r
		return &out
	}
	if legacyInclude == nil {
		return nil
	}
	if *legacyInclude {
		enabled := true
		return &ReasoningConfig{Enabled: &enabled}
	}
	// include_reasoning=false is OpenRouter shorthand for "reason but hide it"
	return &ReasoningConfig{Exclude: true}
}

// IsEnabled reports whether the caller has asked for reasoning at all. The
// provider may still decide to skip based on model capabilities.
func (r *ReasoningConfig) IsEnabled() bool {
	if r == nil {
		return false
	}
	if r.Enabled != nil && !*r.Enabled {
		return false
	}
	return r.Effort != "" || r.MaxTokens > 0 || (r.Enabled != nil && *r.Enabled)
}

// ReasoningDetail is one entry in the OpenRouter-aligned `reasoning_details`
// array. The `Type` field drives which of the content fields is populated.
type ReasoningDetail struct {
	// Type is "reasoning.text", "reasoning.summary", or "reasoning.encrypted".
	Type string `json:"type"`
	// Text is populated for type=reasoning.text.
	Text string `json:"text,omitempty"`
	// Summary is populated for type=reasoning.summary.
	Summary string `json:"summary,omitempty"`
	// Data is populated for type=reasoning.encrypted (base64-encoded blob).
	Data string `json:"data,omitempty"`
	// Signature is a cryptographic signature (Anthropic) when available.
	Signature string `json:"signature,omitempty"`
	// ID is a stable identifier for this detail within the response.
	ID string `json:"id,omitempty"`
	// Format is the origin format so downstream consumers can route:
	// "anthropic-claude-v1", "openai-responses-v1", "google-gemini-v1",
	// "xai-responses-v1", or "unknown".
	Format string `json:"format,omitempty"`
	// Index preserves the ordering across detail chunks.
	Index int `json:"index,omitempty"`
}

type AudioOutput struct {
	ID         string `json:"id,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	Format     string `json:"format,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	Data       string `json:"data,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

// Content handles the union type: string | []ContentPart
type Content struct {
	Text  string
	Parts []ContentPart
}

func (c *Content) UnmarshalJSON(data []byte) error {
	// Try string first
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &c.Text)
	}
	// Try array of parts
	if len(data) > 0 && data[0] == '[' {
		return json.Unmarshal(data, &c.Parts)
	}
	// Null or other?
	return nil
}

func (c Content) MarshalJSON() ([]byte, error) {
	if c.Parts != nil {
		return json.Marshal(c.Parts)
	}
	return json.Marshal(c.Text)
}

type ContentPart struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ImageURL   *ImageURL   `json:"image_url,omitempty"`
	AudioURL   *AudioURL   `json:"audio_url,omitempty"`
	InputAudio *InputAudio `json:"input_audio,omitempty"`
	File       *FileInput  `json:"file,omitempty"`
}

type FileInput struct {
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data"` // URL or data:application/pdf;base64,...
}

type InputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type AudioURL struct {
	URL string `json:"url"`
}

type ResponseFormat struct {
	Type string `json:"type"`
}

type Stop struct {
	Val []string
}

func (s *Stop) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '[' {
		return json.Unmarshal(data, &s.Val)
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	s.Val = []string{str}
	return nil
}

func (s Stop) MarshalJSON() ([]byte, error) {
	if len(s.Val) == 1 {
		return json.Marshal(s.Val[0])
	}
	return json.Marshal(s.Val)
}

type Tool struct {
	Type     string              `json:"type"` // "function"
	Function FunctionDescription `json:"function"`
}

type FunctionDescription struct {
	Description string                 `json:"description,omitempty"`
	Name        string                 `json:"name"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema object
}

type Prediction struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type PluginConfig struct {
	ID      string                 `json:"id"`
	Enabled *bool                  `json:"enabled,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

type ExtensionConfig struct {
	ID      string                 `json:"id"`
	Enabled *bool                  `json:"enabled,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

type ProviderPreferences struct {
	Order             []string `json:"order,omitempty"`
	AllowFallbacks    bool     `json:"allow_fallbacks,omitempty"`
	RequireParameters bool     `json:"require_parameters,omitempty"`
	DataCollection    string   `json:"data_collection,omitempty"` // "deny" | "allow"
}

type DebugOptions struct {
	EchoUpstreamBody bool `json:"echo_upstream_body,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Role string

const (
	User           Role = "user"
	Assistant      Role = "assistant"
	System         Role = "system"
	ModelAssistant Role = "model"
	Anonymous      Role = "anonymous"
)
