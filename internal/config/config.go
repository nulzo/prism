package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/spf13/viper"
)

// ProviderConfig represents the configuration for a single AI provider.
type ProviderConfig struct {
	ID           string                `json:"id" yaml:"id" mapstructure:"id"`
	Type         string                `json:"type" yaml:"type" mapstructure:"type"`
	Name         string                `json:"name" yaml:"name" mapstructure:"name"`
	APIKey       string                `json:"api_key" yaml:"api_key" mapstructure:"api_key"`
	BaseURL      string                `json:"base_url" yaml:"base_url" mapstructure:"base_url"`
	StaticModels []api.ModelDefinition `json:"-" yaml:"-" mapstructure:"-"`
	Config       map[string]string     `json:"config" yaml:"config" mapstructure:"config"`
	Enabled      bool                  `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
}

// RouteConfig allows defining rules for specific models
type RouteConfig struct {
	Pattern  string `json:"pattern" yaml:"pattern" mapstructure:"pattern"`
	TargetID string `json:"target_id" yaml:"target_id" mapstructure:"target_id"`
}

type Config struct {
	Server    ServerConfig          `mapstructure:"server"`
	Redis     RedisConfig           `mapstructure:"redis"`
	RateLimit RateLimitConfig       `mapstructure:"rate_limit"`
	Providers []ProviderConfig      `mapstructure:"providers"`
	Routes    []RouteConfig         `mapstructure:"routes"`
	Models    []api.ModelDefinition `mapstructure:"models"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	Burst             int     `mapstructure:"burst"`
}

type ServerConfig struct {
	Port    string   `mapstructure:"port"`
	Env     string   `mapstructure:"env"`
	APIKeys []string `mapstructure:"api_keys"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
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
	v.SetDefault("server.port", "8080")
	v.SetDefault("server.env", "development")
	v.SetDefault("redis.enabled", false)
	v.SetDefault("rate_limit.requests_per_second", 10.0)
	v.SetDefault("rate_limit.burst", 20)

	// Environment Variables
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Load all models from internal/config/models/*.yaml
	// We need to accumulate them because viper overwrites slices when merging
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
				// Warn but continue
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

	// set the aggregated models back into the main viper instance so Unmarshal works
	v.Set("models", allModels)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unable to decode into struct: %w", err)
	}

	// resolve api keys and inject StaticModels
	for i, p := range cfg.Providers {
		if strings.HasPrefix(p.APIKey, "ENV:") {
			envVar := strings.TrimPrefix(p.APIKey, "ENV:")
			val := os.Getenv(envVar)
			if val == "" {
				val = v.GetString(envVar)
			}
			cfg.Providers[i].APIKey = val
		}

		var providerModels []api.ModelDefinition
		for _, m := range allModels {
			if m.ProviderID == p.ID {
				providerModels = append(providerModels, m)
			}
		}
		cfg.Providers[i].StaticModels = providerModels
	}

	return &cfg, nil
}
