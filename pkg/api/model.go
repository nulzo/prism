package api

type Model struct {
	ID string `json:"id"`
	// CanonicalSlug is the upstream-preferred human slug (e.g.
	// "openai/gpt-4o-mini"). Mirrors OpenRouter's model list response so
	// SDKs that key on canonical_slug resolve without a translation table.
	CanonicalSlug string `json:"canonical_slug,omitempty"`
	// HuggingFaceID points to the model's HF page when available (nominally
	// populated by the catalog for open-weight providers).
	HuggingFaceID string       `json:"hugging_face_id,omitempty"`
	Created       int64        `json:"created"`
	Object        string       `json:"object"`
	OwnedBy       string       `json:"owned_by"`
	Provider      string       `json:"provider,omitempty"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	ContextLength int          `json:"context_length"`
	Architecture  Architecture `json:"architecture"`
	Pricing       Pricing      `json:"pricing"`
	TopProvider   TopProvider  `json:"top_provider"`
	// SupportedParameters lists OpenAI-compat parameter names the model
	// actually honors (e.g. "temperature", "reasoning", "tools"). Optional
	// but useful for UIs that gate feature toggles on capability.
	SupportedParameters []string `json:"supported_parameters,omitempty"`
	// DefaultParameters carries provider-recommended defaults for this
	// model (OpenRouter ships `temperature`, `top_p`, `frequency_penalty`).
	// Typed as a free-form map so new keys don't require a schema change.
	DefaultParameters map[string]interface{} `json:"default_parameters,omitempty"`
	PerRequestLimits  *PerRequestLimits      `json:"per_request_limits,omitempty"`
}

type ModelFilter struct {
	Provider string
	ID       string
	Modality string
	OwnedBy  string
}

type Architecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     string   `json:"instruct_type,omitempty"`
}

type Pricing struct {
	Prompt            string `json:"prompt"`
	Completion        string `json:"completion"`
	Request           string `json:"request"`
	Image             string `json:"image,omitempty"`
	WebSearch         string `json:"web_search,omitempty"`
	InternalReasoning string `json:"internal_reasoning,omitempty"`
	InputCacheRead    string `json:"input_cache_read,omitempty"`
	InputCacheWrite   string `json:"input_cache_write,omitempty"`
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
