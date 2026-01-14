package domain

// ProviderConfig represents the configuration for a single AI provider.
type ProviderConfig struct {
	ID        string            `json:"id" yaml:"id"`                 // Unique ID (e.g., "openai-main")
	Type      string            `json:"type" yaml:"type"`             // Adapter type (e.g., "openai", "anthropic")
	Name      string            `json:"name" yaml:"name"`             // Display name
	APIKey    string            `json:"api_key" yaml:"api_key"`       // Secret key
	BaseURL   string            `json:"base_url" yaml:"base_url"`     // Override URL
	Models    []string          `json:"models" yaml:"models"`         // Explicit model list (optional)
	Config    map[string]string `json:"config" yaml:"config"`         // Extra config (headers, version)
	Enabled   bool              `json:"enabled" yaml:"enabled"`       
}

// RouteConfig allows defining rules for specific models
type RouteConfig struct {
	Pattern  string `json:"pattern" yaml:"pattern"` // Regex or prefix
	TargetID string `json:"target_id" yaml:"target_id"` // Provider ID to use
}
