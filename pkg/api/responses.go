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
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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
