package domain

// ProviderConfig represents the configuration for a single AI provider.
type ProviderConfig struct {
	ID        string            `json:"id" yaml:"id" mapstructure:"id"`
	Type      string            `json:"type" yaml:"type" mapstructure:"type"`
	Name      string            `json:"name" yaml:"name" mapstructure:"name"`
	APIKey    string            `json:"api_key" yaml:"api_key" mapstructure:"api_key"`
	BaseURL   string            `json:"base_url" yaml:"base_url" mapstructure:"base_url"`
	Models    []string          `json:"models" yaml:"models" mapstructure:"models"`
	Config    map[string]string `json:"config" yaml:"config" mapstructure:"config"`
	Enabled   bool              `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
}

// RouteConfig allows defining rules for specific models
type RouteConfig struct {
	Pattern  string `json:"pattern" yaml:"pattern" mapstructure:"pattern"`
	TargetID string `json:"target_id" yaml:"target_id" mapstructure:"target_id"`
}