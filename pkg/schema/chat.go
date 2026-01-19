package schema

import (
	"encoding/json"
)

// --- Request Types ---

type ChatRequest struct {
	// message array is required
	Messages []ChatMessage `json:"messages" binding:"required,min=1"`

	// the model to send request to, generally in shape `<provider>/<model>`
	Model string `json:"model" binding:"required"`

	// Allows to force the model to produce specific output format.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	Stop   *Stop `json:"stop,omitempty"`   // Can be string or []string
	Stream bool  `json:"stream,omitempty"` // Enable streaming

	// LLM Parameters
	MaxTokens         int             `json:"max_tokens,omitempty"`
	Temperature       float64         `json:"temperature,omitempty"`
	TopP              float64         `json:"top_p,omitempty"`
	TopK              int             `json:"top_k,omitempty"` // Not available for OpenAI models
	FrequencyPenalty  float64         `json:"frequency_penalty,omitempty"`
	PresencePenalty   float64         `json:"presence_penalty,omitempty"`
	RepetitionPenalty float64         `json:"repetition_penalty,omitempty"`
	Seed              int             `json:"seed,omitempty"`
	LogitBias         map[int]float64 `json:"logit_bias,omitempty"`
	TopLogprobs       int             `json:"top_logprobs,omitempty"`
	MinP              float64         `json:"min_p,omitempty"`
	TopA              float64         `json:"top_a,omitempty"`

	// Tool calling
	Tools      []Tool      `json:"tools,omitempty"`
	ToolChoice interface{} `json:"tool_choice,omitempty"` // "none", "auto", or object

	// Advanced optional parameters
	Prediction *Prediction `json:"prediction,omitempty"`

	// OpenRouter-only parameters
	Transforms []string             `json:"transforms,omitempty"`
	Models     []string             `json:"models,omitempty"`
	Route      string               `json:"route,omitempty"` // 'fallback'
	Provider   *ProviderPreferences `json:"provider,omitempty"`
	User       string               `json:"user,omitempty"`

	// Debug options
	Debug *DebugOptions `json:"debug,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role" binding:"required,oneof=user assistant system"`
	Content    Content    `json:"content"` // string or []ContentPart
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"` // For assistant messages
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
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type ResponseFormat struct {
	Type string `json:"type"` // e.g., 'json_object'
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

type ProviderPreferences struct {
	Order             []string `json:"order,omitempty"`
	AllowFallbacks    bool     `json:"allow_fallbacks,omitempty"`
	RequireParameters bool     `json:"require_parameters,omitempty"`
	DataCollection    string   `json:"data_collection,omitempty"` // "deny" | "allow"
}

type DebugOptions struct {
	EchoUpstreamBody bool `json:"echo_upstream_body,omitempty"`
}

// --- Response Types ---

type ChatResponse struct {
	ID                string         `json:"id"`
	Choices           []Choice       `json:"choices"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Object            string         `json:"object"` // "chat.completion" or "chat.completion.chunk"
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	Usage             *ResponseUsage `json:"usage,omitempty"`
}

type Choice struct {
	Index              int            `json:"index"`
	Message            *ChatMessage   `json:"message,omitempty"` // For non-streaming
	Delta              *ChatMessage   `json:"delta,omitempty"`   // For streaming
	FinishReason       string         `json:"finish_reason"`
	NativeFinishReason string         `json:"native_finish_reason,omitempty"`
	Error              *ErrorResponse `json:"error,omitempty"`
}

type ResponseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ErrorResponse struct {
	Code     int                    `json:"code"`
	Message  string                 `json:"message"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}
