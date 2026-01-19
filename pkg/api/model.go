package api

type Model struct {
	ID               string            `json:"id"`
	Created          int64             `json:"created"`
	Object           string            `json:"object"`
	OwnedBy          string            `json:"owned_by"`
	Provider         string            `json:"provider,omitempty"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	ContextLength    int               `json:"context_length"`
	Architecture     Architecture      `json:"architecture"`
	Pricing          Pricing           `json:"pricing"`
	TopProvider      TopProvider       `json:"top_provider"`
	PerRequestLimits *PerRequestLimits `json:"per_request_limits,omitempty"`
}

type Architecture struct {
	Modality     string `json:"modality"`
	Tokenizer    string `json:"tokenizer"`
	InstructType string `json:"instruct_type,omitempty"`
}

type Pricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
	Image      string `json:"image,omitempty"`
	Request    string `json:"request,omitempty"`
}

type TopProvider struct {
	ContextLength       int  `json:"context_length"`
	MaxCompletionTokens int  `json:"max_completion_tokens,omitempty"`
	IsModerated         bool `json:"is_moderated"`
}

type PerRequestLimits struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
}
