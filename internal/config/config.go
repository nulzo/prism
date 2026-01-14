package config

import "os"

// Config holds the application configuration.
type Config struct {
	ServerPort string
	
	// API Keys
	OpenAIKey    string
	AnthropicKey string
	GoogleKey    string

	// Base URLs (can be overridden for testing/proxying)
	OpenAIBaseURL    string
	AnthropicBaseURL string
	GoogleBaseURL    string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() *Config {
	return &Config{
		ServerPort:       getEnv("SERVER_PORT", "8080"),
		OpenAIKey:        getEnv("OPENAI_API_KEY", "sk-mock-openai"),
		AnthropicKey:     getEnv("ANTHROPIC_API_KEY", "sk-mock-anthropic"),
		GoogleKey:        getEnv("GOOGLE_API_KEY", "mock-google-key"),
		OpenAIBaseURL:    getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		AnthropicBaseURL: getEnv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		GoogleBaseURL:    getEnv("GOOGLE_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
