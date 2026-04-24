package api

import "time"

// ModelDefinition represents the static configuration for a model in models.yaml
type ModelDefinition struct {
	ID          string       `mapstructure:"id" json:"id"`                   // Public ID (e.g., "openai/gpt-4")
	Name        string       `mapstructure:"name" json:"name"`               // Human readable name
	ProviderID  string       `mapstructure:"provider_id" json:"provider_id"` // internal provider reference (e.g. "openai-main")
	UpstreamID  string       `mapstructure:"upstream_id" json:"upstream_id"` // The ID sent to the upstream provider
	Description string       `mapstructure:"description" json:"description"`
	Pricing     ModelPricing `mapstructure:"pricing" json:"pricing"`
	Config      ModelConfig  `mapstructure:"config" json:"config"`
	Enabled     bool         `mapstructure:"enabled" json:"enabled"`

	// OpenRouter aligned fields
	ContextLength int               `mapstructure:"context_length" json:"context_length"`
	Architecture  ModelArchitecture `mapstructure:"architecture" json:"architecture"`
	TopProvider   ModelTopProvider  `mapstructure:"top_provider" json:"top_provider"`

	// CanonicalSlug is the vendor's preferred model identifier (e.g.
	// "openai/gpt-4o"). Defaults to ID when unset so every listing has a
	// stable slug for SDKs to pin.
	CanonicalSlug string `mapstructure:"canonical_slug" json:"canonical_slug,omitempty"`
	// HuggingFaceID points to the model's HF page for open-weight catalogues.
	HuggingFaceID string `mapstructure:"hugging_face_id" json:"hugging_face_id,omitempty"`
	// SupportedParameters is an advisory capability list so UIs can show or
	// hide feature toggles (e.g. "reasoning" only for models that honor it).
	SupportedParameters []string `mapstructure:"supported_parameters" json:"supported_parameters,omitempty"`
	// DefaultParameters is a free-form map of recommended defaults keyed
	// by the OpenAI-compat parameter name. Intentionally untyped so
	// providers can ship arbitrary defaults without a schema bump.
	DefaultParameters map[string]interface{} `mapstructure:"default_parameters" json:"default_parameters,omitempty"`

	PerRequestLimits *PerRequestLimits `mapstructure:"per_request_limits" json:"per_request_limits,omitempty"`
	KnowledgeCutoff  *string           `mapstructure:"knowledge_cutoff" json:"knowledge_cutoff,omitempty"`
	ExpirationDate   *string           `mapstructure:"expiration_date" json:"expiration_date,omitempty"`
	Links            map[string]string `mapstructure:"links" json:"links,omitempty"`

	// Metadata for management
	Source      string    `mapstructure:"source" json:"source"` // "auto" or "manual"
	LastUpdated time.Time `mapstructure:"last_updated" json:"last_updated"`
}

type ModelPricing struct {
	Prompt            string `mapstructure:"prompt" json:"prompt"`
	Completion        string `mapstructure:"completion" json:"completion"`
	Request           string `mapstructure:"request" json:"request"`
	Image             string `mapstructure:"image" json:"image"`
	WebSearch         string `mapstructure:"web_search" json:"web_search"`
	InternalReasoning string `mapstructure:"internal_reasoning" json:"internal_reasoning"`
	InputCacheRead    string `mapstructure:"input_cache_read" json:"input_cache_read"`
	InputCacheWrite   string `mapstructure:"input_cache_write" json:"input_cache_write"`
}

type ModelArchitecture struct {
	InputModalities  []string `mapstructure:"input_modalities" json:"input_modalities"`
	OutputModalities []string `mapstructure:"output_modalities" json:"output_modalities"`
	Tokenizer        string   `mapstructure:"tokenizer" json:"tokenizer"`
	InstructType     string   `mapstructure:"instruct_type" json:"instruct_type"`
}

type ModelTopProvider struct {
	ContextLength       int  `mapstructure:"context_length" json:"context_length"`
	MaxCompletionTokens int  `mapstructure:"max_completion_tokens" json:"max_completion_tokens"`
	IsModerated         bool `mapstructure:"is_moderated" json:"is_moderated"`
}

type ModelConfig struct {
	ContextWindow    int      `mapstructure:"context_window" json:"context_window"`
	MaxOutput        int      `mapstructure:"max_output" json:"max_output"`
	Modality         []string `mapstructure:"modality" json:"modality"` // text, image, audio
	ImageSupport     bool     `mapstructure:"image_support" json:"image_support"`
	ToolUse          bool     `mapstructure:"tool_use" json:"tool_use"`
	StreamingSupport bool     `mapstructure:"streaming_support" json:"streaming_support"`
}
