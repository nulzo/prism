package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/spf13/viper"
)

// ProviderConfig represents the configuration for a single AI provider.
type ProviderConfig struct {
	ID           string                `json:"id" yaml:"id" mapstructure:"id" validate:"required"`
	Type         string                `json:"type" yaml:"type" mapstructure:"type" validate:"required,oneof=openai anthropic google ollama"`
	Name         string                `json:"name" yaml:"name" mapstructure:"name" validate:"required"`
	APIKey       string                `json:"api_key" yaml:"api_key" mapstructure:"api_key" validate:"required_if=RequiresAuth true"`
	BaseURL      string                `json:"base_url" yaml:"base_url" mapstructure:"base_url" validate:"omitempty,url"`
	StaticModels []api.ModelDefinition `json:"-" yaml:"-" mapstructure:"-"`
	Config       map[string]string     `json:"config" yaml:"config" mapstructure:"config"`
	Enabled      bool                  `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	RequiresAuth bool                  `json:"requires_auth" yaml:"requires_auth" mapstructure:"requires_auth"`
}

// RouteConfig allows defining rules for specific models
type RouteConfig struct {
	Pattern  string `json:"pattern" yaml:"pattern" mapstructure:"pattern" validate:"required"`
	TargetID string `json:"target_id" yaml:"target_id" mapstructure:"target_id" validate:"required"`
}

type Config struct {
	Server    ServerConfig          `mapstructure:"server" validate:"required"`
	Redis     RedisConfig           `mapstructure:"redis" validate:"required"`
	RateLimit RateLimitConfig       `mapstructure:"rate_limit" validate:"required"`
	Database  DatabaseConfig        `mapstructure:"database" validate:"required"`
	Providers []ProviderConfig      `mapstructure:"providers"`
	Routes    []RouteConfig         `mapstructure:"routes" validate:"dive"`
	Models    []api.ModelDefinition `mapstructure:"models"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second" validate:"gt=0"`
	Burst             int     `mapstructure:"burst" validate:"gt=0"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path" validate:"required"`
}

type ServerConfig struct {
	Port        int      `mapstructure:"port" validate:"required,numeric"`
	Env         string   `mapstructure:"env" validate:"required,oneof=development production staging"`
	AuthEnabled bool     `mapstructure:"auth_enabled"`
	APIKeys     []string `mapstructure:"api_keys" validate:"dive,min=10"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr" validate:"required_if=Enabled true"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db" validate:"min=0"`
	Enabled  bool   `mapstructure:"enabled"`
}

// LoadConfig reads configuration from file or environment variables.
func LoadConfig() (*Config, error) {
	// Load .env file if present
	_ = godotenv.Load()

	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("./internal/config")

	// Default Values
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.env", "development")
	v.SetDefault("redis.enabled", false)
	v.SetDefault("rate_limit.requests_per_second", 10.0)
	v.SetDefault("rate_limit.burst", 20)
	v.SetDefault("database.path", "./router.db")

	// Environment Variables
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Load models
	allModels := loadModels()
	v.Set("models", allModels)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unable to decode into struct: %w", err)
	}

	// Resolve dynamic values and internal mapping
	resolveConfiguration(&cfg, v, allModels)

	// Validate the configuration
	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// resolveConfiguration handles post-load logic like env var injection and model mapping
func resolveConfiguration(cfg *Config, v *viper.Viper, allModels []api.ModelDefinition) {
	for i, p := range cfg.Providers {
		// Handle ENV: prefix for API keys
		if strings.HasPrefix(p.APIKey, "ENV:") {
			envVar := strings.TrimPrefix(p.APIKey, "ENV:")
			val := os.Getenv(envVar)
			if val == "" {
				val = v.GetString(envVar)
			}
			cfg.Providers[i].APIKey = val
		}

		// Handle ENV: prefix for BaseURL
		if strings.HasPrefix(p.BaseURL, "ENV:") {
			envVar := strings.TrimPrefix(p.BaseURL, "ENV:")
			val := os.Getenv(envVar)
			if val == "" {
				val = v.GetString(envVar)
			}
			cfg.Providers[i].BaseURL = val
		}

		// Inject static models
		var providerModels []api.ModelDefinition
		for _, m := range allModels {
			if m.ProviderID == p.ID {
				providerModels = append(providerModels, m)
			}
		}
		cfg.Providers[i].StaticModels = providerModels
	}
}

// loadModels discovers and loads model definitions from yaml files
func loadModels() []api.ModelDefinition {
	var allModels []api.ModelDefinition

	// Try to find the models directory relative to execution or common paths
	modelSearchPaths := []string{
		"internal/config/models/*.yaml",
		"./config/models/*.yaml",
		"./models/*.yaml",
	}

	for _, pattern := range modelSearchPaths {
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			vModel := viper.New()
			vModel.SetConfigFile(file)
			if err := vModel.ReadInConfig(); err != nil {
				// Warn but continue - using fmt here as we don't have logger injected
				fmt.Printf("Warning: Failed to read model config %s: %v\n", file, err)
				continue
			}

			var fileData struct {
				Models []api.ModelDefinition `mapstructure:"models"`
			}
			if err := vModel.Unmarshal(&fileData); err == nil {
				allModels = append(allModels, fileData.Models...)
			}
		}
	}
	return allModels
}
