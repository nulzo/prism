package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/server"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store/cache"

	// "github.com/nulzo/model-gateway-server/internal/otel"
	"github.com/nulzo/model-router-api/internal/gateway"
	"go.uber.org/zap"

	_ "github.com/nulzo/model-router-api/internal/llm/anthropic"
	_ "github.com/nulzo/model-router-api/internal/llm/google"
	_ "github.com/nulzo/model-router-api/internal/llm/ollama"
	_ "github.com/nulzo/model-router-api/internal/llm/openai"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		panic("failed to load configuration: " + err.Error())
	}

	logger.Initialize(cfg.Server.Env)
	defer logger.Sync()

	logger.Info("Starting Model Router API", zap.String("env", cfg.Server.Env), zap.String("port", cfg.Server.Port))

	validator.InitValidator()

	// shutdownTracer, err := otel.InitTracer("model-gateway-server", logger.Get(), os.Stdout)
	// if err != nil {
	// 	logger.Error("Failed to initialize tracer", zap.Error(err))
	// } else {
	// 	defer func() {
	// 		if err := shutdownTracer(context.Background()); err != nil {
	// 			logger.Error("Failed to shutdown tracer", zap.Error(err))
	// 		}
	// 	}()
	// }

	var cacheService cache.CacheService
	if cfg.Redis.Enabled {
		logger.Info("Using Redis Cache", zap.String("addr", cfg.Redis.Addr))
		cacheService = cache.NewRedisCache(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	} else {
		logger.Info("Using Memory Cache")
		cacheService = cache.NewMemoryCache()
	}

	log := logger.Get()

	routerService := gateway.NewService(log, cacheService)

	registeredCount := 0
	ctx := context.Background()

	for _, pCfg := range cfg.Providers {
		if !pCfg.Enabled {
			continue
		}

		factoryFunc, err := llm.Get(pCfg.Type)
		if err != nil {
			log.Error("Unknown provider type", zap.String("type", pCfg.Type))
			continue
		}

		providerInstance, err := factoryFunc(pCfg)
		if err != nil {
			log.Error("Failed to initialize provider",
				zap.String("id", pCfg.ID),
				zap.Error(err),
			)
			continue
		}

		// C. Register with Service
		if err := routerService.RegisterProvider(ctx, providerInstance); err != nil {
			log.Error("Failed to register provider", zap.String("id", pCfg.ID), zap.Error(err))
			continue
		}

		registeredCount++
	}

	if registeredCount == 0 {
		logger.Warn("No providers were registered. API will not function correctly.")
	}

	apiServer := server.NewServer(cfg, log, svc)
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
