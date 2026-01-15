package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/adapters/cache/memory"
	"github.com/nulzo/model-router-api/internal/adapters/cache/redis"
	"github.com/nulzo/model-router-api/internal/adapters/http/middleware"
	v1 "github.com/nulzo/model-router-api/internal/adapters/http/v1"
	"github.com/nulzo/model-router-api/internal/adapters/providers/factory"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/core/services"
	"github.com/nulzo/model-router-api/internal/logger"
	"go.uber.org/zap"

	// Import providers to trigger init() registration
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/anthropic"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/google"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/ollama"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/openai"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		// Panic here because we can't start without config
		panic("failed to load configuration: " + err.Error())
	}

	// 2. Initialize Logger
	logger.Initialize(cfg.Server.Env)
	defer logger.Sync()

	logger.Info("Starting Model Router API", zap.String("env", cfg.Server.Env), zap.String("port", cfg.Server.Port))

	// 3. Initialize Cache
	var cacheService ports.CacheService
	if cfg.Redis.Enabled {
		logger.Info("Using Redis Cache", zap.String("addr", cfg.Redis.Addr))
		cacheService = redis.NewRedisCache(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	} else {
		logger.Info("Using Memory Cache")
		cacheService = memory.NewMemoryCache()
	}

	// 4. Initialize Core Services
	routerService := services.NewRouterService(cacheService)
	providerFactory := factory.NewProviderFactory()

	// 5. Register Providers from Config
	registeredCount := 0
	for _, pCfg := range cfg.Providers {
		if !pCfg.Enabled {
			continue
		}

		p, err := providerFactory.CreateProvider(pCfg)
		if err != nil {
			logger.Error("Failed to create provider", 
				zap.String("id", pCfg.ID), 
				zap.String("type", pCfg.Type), 
				zap.Error(err))
			continue
		}
		
		routerService.RegisterProvider(p)
		if len(pCfg.Models) > 0 {
			routerService.RegisterModels(pCfg.ID, pCfg.Models)
		}
		logger.Info("Registered provider", zap.String("name", pCfg.Name), zap.String("id", pCfg.ID), zap.Int("models_count", len(pCfg.Models)))
		registeredCount++
	}
	
	if registeredCount == 0 {
		logger.Warn("No providers were registered. API will not function correctly.")
	}

	// 6. Set Routing Rules
	routerService.SetRoutes(cfg.Routes)

	// 7. Setup HTTP Server
	if cfg.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	
	// Middleware
	engine.Use(middleware.StructuredLogger())
	engine.Use(gin.Recovery())
	engine.Use(middleware.CORSMiddleware())

	handler := v1.NewHandler(routerService)
	v1Group := engine.Group("/v1")
	
	// Add Auth/RateLimit here if needed, driven by config
	// v1Group.Use(middleware.AuthMiddleware(...))

	handler.RegisterRoutes(v1Group)

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: engine,
	}

	// Graceful Shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("Server start failure", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exiting")
}