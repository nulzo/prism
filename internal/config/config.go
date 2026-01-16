package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/joho/godotenv"
)

type Config struct {
	Server    ServerConfig            `mapstructure:"server"`
	Redis     RedisConfig             `mapstructure:"redis"`
	RateLimit RateLimitConfig         `mapstructure:"rate_limit"`
	Providers []domain.ProviderConfig `mapstructure:"providers"`
	Routes    []domain.RouteConfig    `mapstructure:"routes"`
	Models    []schema.ModelDefinition `mapstructure:"models"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	Burst             int     `mapstructure:"burst"`
}

type ServerConfig struct {
	Port string `mapstructure:"port"`
	Env  string `mapstructure:"env"`
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
	var allModels []schema.ModelDefinition
	
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
				Models []schema.ModelDefinition `mapstructure:"models"`
			}
			if err := vModel.Unmarshal(&fileData); err == nil {
				allModels = append(allModels, fileData.Models...)
			}
		}
	}

	// Set the aggregated models back into the main viper instance so Unmarshal works
	v.Set("models", allModels)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unable to decode into struct: %w", err)
	}
	
	// Resolve API Keys
	for i, p := range cfg.Providers {
		if strings.HasPrefix(p.APIKey, "ENV:") {
			envVar := strings.TrimPrefix(p.APIKey, "ENV:")
			// Check process environment first (explicit override)
			val := os.Getenv(envVar)
			if val == "" {
				// Then check viper (which might have it from other sources)
				val = v.GetString(envVar)
			}
			cfg.Providers[i].APIKey = val
		}
	}

	return &cfg, nil
}
