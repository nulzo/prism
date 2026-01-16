package schema

import "time"

// ModelDefinition represents the static configuration for a model in models.yaml
type ModelDefinition struct {
	ID          string       `mapstructure:"id" json:"id"`                     // Public ID (e.g., "openai/gpt-4")
	Name        string       `mapstructure:"name" json:"name"`                 // Human readable name
	ProviderID  string       `mapstructure:"provider_id" json:"provider_id"`   // internal provider reference (e.g. "openai-main")
	UpstreamID  string       `mapstructure:"upstream_id" json:"upstream_id"`   // The ID sent to the upstream provider
	Description string       `mapstructure:"description" json:"description"`
	Pricing     ModelPricing `mapstructure:"pricing" json:"pricing"`
	Config      ModelConfig  `mapstructure:"config" json:"config"`
	Enabled     bool         `mapstructure:"enabled" json:"enabled"`
	
	// Metadata for management
	Source      string    `mapstructure:"source" json:"source"`           // "auto" or "manual"
	LastUpdated time.Time `mapstructure:"last_updated" json:"last_updated"`
}

type ModelPricing struct {
	Input  float64 `mapstructure:"input" json:"input"`   // Cost per 1M tokens
	Output float64 `mapstructure:"output" json:"output"` // Cost per 1M tokens
	Image  float64 `mapstructure:"image" json:"image"`   // Cost per image (if applicable)
}

type ModelConfig struct {
	ContextWindow    int      `mapstructure:"context_window" json:"context_window"`
	MaxOutput        int      `mapstructure:"max_output" json:"max_output"`
	Modality         []string `mapstructure:"modality" json:"modality"` // text, image, audio
	ImageSupport     bool     `mapstructure:"image_support" json:"image_support"`
	ToolUse          bool     `mapstructure:"tool_use" json:"tool_use"`
	StreamingSupport bool     `mapstructure:"streaming_support" json:"streaming_support"`
}