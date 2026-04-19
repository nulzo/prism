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

type ChatMessage struct {
	Role        string        `json:"role" binding:"required,oneof=user assistant system tool"`
	Content     Content       `json:"content"` // string or []ContentPart
	Reasoning   string        `json:"reasoning,omitempty"`
	Name        string        `json:"name,omitempty"`
	ToolCallID  string        `json:"tool_call_id,omitempty"`
	ToolCalls   []ToolCall    `json:"tool_calls,omitempty"`  // For assistant messages
	Images      []ContentPart `json:"images,omitempty"`      // For image generation results
	Audio       *AudioOutput  `json:"audio,omitempty"`       // For audio generation results
	Annotations []interface{} `json:"annotations,omitempty"` // For file parsing results, etc.
}

type AudioOutput struct {
	ID         string `json:"id,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
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
