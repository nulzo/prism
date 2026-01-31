package api

type ChatResponse struct {
	ID                string         `json:"id"`
	Choices           []Choice       `json:"choices"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Object            string         `json:"object"` // "chat.completion" or "chat.completion.chunk"
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	Usage             *ResponseUsage `json:"usage,omitempty"`

	Error *ErrorResponse `json:"error,omitempty"`
}

func (e *ErrorResponse) Error() string {
	return e.Message
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
	// Standard Token Counts
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Token Breakdown
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`

	// Financials
	Cost        *float64     `json:"cost,omitempty"` // Cost in credits
	IsBYOK      *bool        `json:"is_byok,omitempty"`
	CostDetails *CostDetails `json:"cost_details,omitempty"`

	// Server Tool Usage
	ServerToolUse *ServerToolUse `json:"server_tool_use,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	AudioTokens      int `json:"audio_tokens,omitempty"`
	VideoTokens      int `json:"video_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	ImageTokens     int `json:"image_tokens,omitempty"`
}

type CostDetails struct {
	UpstreamInferenceCost           *float64 `json:"upstream_inference_cost,omitempty"` // BYOK only
	UpstreamInferencePromptCost     float64  `json:"upstream_inference_prompt_cost"`
	UpstreamInferenceCompletionCost float64  `json:"upstream_inference_completions_cost"`
}

type ServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests,omitempty"`
}

type ErrorResponse struct {
	Code     interface{}            `json:"code,omitempty"`
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

type StreamResult struct {
	Response *ChatResponse
	Err      error
}
