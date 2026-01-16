package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/joho/godotenv"
)

type Config struct {
	Server    ServerConfig            `mapstructure:"server"`
	Redis     RedisConfig             `mapstructure:"redis"`
	RateLimit RateLimitConfig         `mapstructure:"rate_limit"`
	Providers []domain.ProviderConfig `mapstructure:"providers"`
	Routes    []domain.RouteConfig    `mapstructure:"routes"`
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
