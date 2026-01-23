package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/server"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"go.uber.org/zap"

	_ "github.com/nulzo/model-router-api/internal/llm/anthropic"
	_ "github.com/nulzo/model-router-api/internal/llm/google"
	_ "github.com/nulzo/model-router-api/internal/llm/ollama"
	_ "github.com/nulzo/model-router-api/internal/llm/openai"
)

// Version is the version of the application. We inject this during
// the docker build stage.
var Version = "snapshot"

const rawBanner = `
   ________   _______   ________  ________  _______  
  ╱        ╲╱╱       ╲ ╱        ╲╱        ╲╱       ╲╲
 ╱         ╱╱        ╱_╱       ╱╱        _╱        ╱╱
╱╱      __╱        _╱╱         ╱-        ╱         ╱ 
╲╲_____╱  ╲____╱___╱ ╲╲_______╱╲_______╱╱╲__╱__╱__╱  
`

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		panic("failed to load configuration: " + err.Error())
	}

	// 1. Print the Banner immediately upon startup
	printBanner(cfg.Server.Port, cfg.Server.Env)

	logger.Initialize(logger.DefaultConfig())
	defer logger.Sync()

	validator.InitValidator()

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

		if err := routerService.RegisterProvider(ctx, providerInstance); err != nil {
			log.Error("Failed to register provider", zap.String("id", pCfg.ID), zap.Error(err))
			continue
		}

		registeredCount++
	}

	if registeredCount == 0 {
		logger.Warn("No providers were registered. API will not function correctly.")
	}

	apiServer := server.New(cfg, log, routerService)

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: apiServer.Handler(),
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

// printBanner shows a pretty banner in the CLI on startup
func printBanner(port, env string) {
	lines := strings.Split(rawBanner, "\n")
	// Remove empty leading line if it exists
	if len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}

	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Calculate ratio (0.0 to 1.0) for the gradient
		ratio := float64(i) / float64(len(lines)-1)
		if len(lines) == 1 {
			ratio = 0
		}

		// Use the cli module to generate the gradient string
		// This respects NO_COLOR environment variables automatically
		fmt.Println(cli.Gradient(line, cli.BrandBlue, cli.BrandPurple, ratio))
	}

	fmt.Println()
	// Use cli.Style for bolding valuable information
	// Use cli.Arrow for a consistent graphical element
	fmt.Printf("   Version:     %s\n", cli.Style(Version, cli.Bold))
	fmt.Printf("   Go Version:  %s\n", runtime.Version())
	fmt.Printf("   Environment: %s\n", cli.Style(env, cli.Bold))
	fmt.Printf("   Port:        %s\n", cli.Style(port, cli.Bold))
	fmt.Printf("   Github:      %s\n", cli.Style("https://github.com/nulzo/prism", cli.Bold))
	fmt.Println("   --------------------------------------------------")
	fmt.Println()
}
