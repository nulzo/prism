package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/adapters/http/middleware"
	"github.com/nulzo/model-router-api/internal/adapters/http/v1"
	"github.com/nulzo/model-router-api/internal/adapters/providers/factory"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/services"

	// Import providers to trigger init() registration
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/anthropic"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/google"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/ollama"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/openai"
)

func main() {
	// 1. Load Base Config (Env vars)
	cfg := config.LoadConfig()

	// 2. Define Providers (Ideally loaded from a DB or YAML file)
	// Now purely data-driven. The system knows nothing about "openai" or "google" packages directly here.
	providerConfigs := []domain.ProviderConfig{
		{
			ID:      "openai-main",
			Type:    "openai",
			Name:    "OpenAI",
			APIKey:  cfg.OpenAIKey,
			BaseURL: cfg.OpenAIBaseURL,
		},
		{
			ID:      "anthropic-main",
			Type:    "anthropic",
			Name:    "Anthropic",
			APIKey:  cfg.AnthropicKey,
			BaseURL: cfg.AnthropicBaseURL,
			Config:  map[string]string{"version": "2023-06-01"},
		},
		{
			ID:      "google-main",
			Type:    "google",
			Name:    "Google Gemini",
			APIKey:  cfg.GoogleKey,
			BaseURL: cfg.GoogleBaseURL,
		},
		{
			ID:      "deepseek-main",
			Type:    "openai", // Reusing OpenAI adapter
			Name:    "DeepSeek",
			APIKey:  "sk-mock-deepseek",
			BaseURL: "https://api.deepseek.com/v1",
		},
		{
			ID:      "local-ollama",
			Type:    "ollama",
			Name:    "Ollama Local",
			BaseURL: cfg.OllamaBaseURL,
		},
	}

	// 3. Initialize Core Services
	routerService := services.NewRouterService()
	providerFactory := factory.NewProviderFactory()

	// 4. Register Providers using the Factory (which uses the Registry)
	for _, pCfg := range providerConfigs {
		// This now looks up the "Type" in the registry and invokes the constructor
		p, err := providerFactory.CreateProvider(pCfg)
		if err != nil {
			log.Printf("Failed to create provider %s (type: %s): %v", pCfg.ID, pCfg.Type, err)
			continue
		}
		routerService.RegisterProvider(p)
		log.Printf("Registered provider: %s (%s)", pCfg.Name, pCfg.ID)
	}

	// 5. Define Routing Rules (Again, could be DB driven)
	routerService.SetRoutes([]domain.RouteConfig{
		{Pattern: "gpt-4", TargetID: "openai-main"},
		{Pattern: "claude", TargetID: "anthropic-main"},
		{Pattern: "gemini", TargetID: "google-main"},
		{Pattern: "deepseek", TargetID: "deepseek-main"},
		{Pattern: "llama", TargetID: "local-ollama"},
		{Pattern: "mistral", TargetID: "local-ollama"},
	})

	// 6. Setup Handlers & Server
	handler := v1.NewHandler(routerService)
	engine := gin.New() // Use New() to control middleware order explicitly
	
	// Global Middleware
	engine.Use(middleware.StructuredLogger())
	engine.Use(gin.Recovery())
	engine.Use(middleware.CORSMiddleware())
	
	// Define API Key(s) for the router itself (e.g., provided to clients)
	// In production, load from env or DB
	validKeys := []string{"sk-router-admin-key", "sk-router-client-key"}
	
	v1Group := engine.Group("/v1")
	
	// Apply Auth and Rate Limit specifically to the API group
	v1Group.Use(middleware.AuthMiddleware(validKeys))
	v1Group.Use(middleware.RateLimitMiddleware(100, 200)) // 100 RPS, burst 200

	handler.RegisterRoutes(v1Group)

	log.Printf("Starting Enterprise Model Router on port %s", cfg.ServerPort)
	if err := engine.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
