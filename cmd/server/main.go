package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nulzo/model-router-api/internal/adapters/cache/memory"
	"github.com/nulzo/model-router-api/internal/adapters/cache/redis"
	"github.com/nulzo/model-router-api/internal/adapters/providers/factory"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/core/services"
	"github.com/nulzo/model-router-api/internal/logger"
	"github.com/nulzo/model-router-api/internal/otel"
	"github.com/nulzo/model-router-api/internal/router"
	"go.uber.org/zap"

	_ "github.com/nulzo/model-router-api/internal/adapters/providers/anthropic"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/google"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/ollama"
	_ "github.com/nulzo/model-router-api/internal/adapters/providers/openai"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		panic("failed to load configuration: " + err.Error())
	}

	logger.Initialize(cfg.Server.Env)
	defer logger.Sync()

	logger.Info("Starting Model Router API", zap.String("env", cfg.Server.Env), zap.String("port", cfg.Server.Port))

	shutdownTracer, err := otel.InitTracer("model-router-api", logger.Get(), os.Stdout)
	if err != nil {
		logger.Error("Failed to initialize tracer", zap.Error(err))
	} else {
		defer func() {
			if err := shutdownTracer(context.Background()); err != nil {
				logger.Error("Failed to shutdown tracer", zap.Error(err))
			}
		}()
	}

	var cacheService ports.CacheService
	if cfg.Redis.Enabled {
		logger.Info("Using Redis Cache", zap.String("addr", cfg.Redis.Addr))
		cacheService = redis.NewRedisCache(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	} else {
		logger.Info("Using Memory Cache")
		cacheService = memory.NewMemoryCache()
	}

	routerService := services.NewRouterService(cacheService)
	providerFactory := factory.NewProviderFactory()

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

	routerService.SetRoutes(cfg.Routes)

	appRouter := router.New(cfg, logger.Get(), routerService)
	engine := appRouter.Setup()

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: engine,
	}

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
