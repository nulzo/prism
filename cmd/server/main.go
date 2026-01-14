package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/api"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/internal/provider/anthropic"
	"github.com/nulzo/model-router-api/internal/provider/google"
	"github.com/nulzo/model-router-api/internal/provider/ollama"
	"github.com/nulzo/model-router-api/internal/provider/openai"
	"github.com/nulzo/model-router-api/internal/router"
)

func main() {
	cfg := config.LoadConfig()

	openaiP := openai.NewAdapter(provider.ProviderConfig{
		APIKey:  cfg.OpenAIKey,
		BaseURL: cfg.OpenAIBaseURL,
	})

	anthropicP := anthropic.NewAdapter(provider.ProviderConfig{
		APIKey:  cfg.AnthropicKey,
		BaseURL: cfg.AnthropicBaseURL,
		Version: "2023-06-01",
	})

	googleP := google.NewAdapter(provider.ProviderConfig{
		APIKey:  cfg.GoogleKey,
		BaseURL: cfg.GoogleBaseURL,
	})

	ollamaP := ollama.NewAdapter(provider.ProviderConfig{
		BaseURL: cfg.OllamaBaseURL,
	})

	r := router.NewRouter()
	r.RegisterProvider(openaiP)
	r.RegisterProvider(anthropicP)
	r.RegisterProvider(googleP)
	r.RegisterProvider(ollamaP)

	// OpenAI
	r.RegisterRoute("gpt-4", "openai")
	r.RegisterRoute("gpt-5", "openai") // Catches gpt-5.2, gpt-5-turbo
	r.RegisterRoute("o1", "openai")    // OpenAI 'o1' reasoning models

	// Anthropic
	r.RegisterRoute("claude-3", "anthropic")
	r.RegisterRoute("claude-4", "anthropic") // Catches claude-4.5-opus

	// Google
	r.RegisterRoute("gemini", "google") // Catches gemini-1.5, gemini-2.0

	// Ollama (since it's local, we might want to register specific local models or a catch-all)
	r.RegisterRoute("llama", "ollama")
	r.RegisterRoute("deepseek", "ollama")
	r.RegisterRoute("mistral", "ollama")

	handler := api.NewHandler(r)

	engine := gin.Default()
	handler.RegisterRoutes(engine)

	log.Printf("Starting Enterprise Model Router on port %s", cfg.ServerPort)
	if err := engine.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
