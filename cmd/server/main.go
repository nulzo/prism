package main

import (
	"context"
	"errors"
	_ "expvar"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/internal/catalog"
	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	_ "github.com/nulzo/model-router-api/internal/llm/anthropic"
	_ "github.com/nulzo/model-router-api/internal/llm/bfl"
	_ "github.com/nulzo/model-router-api/internal/llm/cosyvoice"
	_ "github.com/nulzo/model-router-api/internal/llm/elevenlabs"
	_ "github.com/nulzo/model-router-api/internal/llm/google"
	_ "github.com/nulzo/model-router-api/internal/llm/moonshot"
	_ "github.com/nulzo/model-router-api/internal/llm/ollama"
	_ "github.com/nulzo/model-router-api/internal/llm/openai"
	_ "github.com/nulzo/model-router-api/internal/llm/qwen3"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/server"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/internal/store/sqlite"
	"go.uber.org/zap"
)

// Version is the version of the application. We inject this during
// the docker build stage.
var Version = "snapshot"

// parseDurationOr returns the parsed duration or the provided fallback when
// the input is empty or unparsable. Keeping the helper in main.go avoids
// bleeding config-shape concerns into the catalog package itself.
func parseDurationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

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

	printBanner(fmt.Sprintf("%d", cfg.Server.Port), cfg.Server.Env)

	log, err := logger.New(logger.DefaultConfig())
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	logger.SetGlobal(log)
	defer func() {
		_ = log.Sync()
	}()

	val := validator.New()

	var cacheService cache.CacheService
	if cfg.Redis.Enabled {
		log.Info("Using Redis Cache", zap.String("addr", cfg.Redis.Addr))
		cacheService = cache.NewRedisCache(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	} else {
		log.Info("Using Memory Cache")
		cacheService = cache.NewMemoryCache()
	}

	// Initialize Database
	repo, err := sqlite.NewSQLiteStorage(cfg.Database.Path, log)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer func() {
		_ = repo.Close()
	}()

	// Sync *providers* to the DB up front. Models sync is handled by the
	// catalog's DB sink on every hydrate so we don't duplicate work here.
	ctx := context.Background()
	if err := repo.WithTx(ctx, func(r store.Repository) error {
		dbProviders := make([]model.Provider, 0, len(cfg.Providers))
		for _, p := range cfg.Providers {
			dbProviders = append(dbProviders, model.Provider{
				ID:         p.ID,
				Name:       p.ID,
				BaseURL:    "config",
				IsEnabled:  p.Enabled,
				Priority:   0,
				ConfigJSON: "{}",
			})
		}
		return r.Providers().SyncProviders(ctx, dbProviders)
	}); err != nil {
		logger.Fatal("Failed to sync providers", zap.Error(err))
	}

	// Initialize Analytics Ingestor
	ingestor := analytics.NewIngestor(log, repo)
	ingestor.Start(context.Background())
	defer ingestor.Stop()

	// Parse catalog timings with sane defaults so the config stays optional.
	refreshInterval := parseDurationOr(cfg.Catalog.RefreshInterval, 15*time.Minute)
	hydrateTimeout := parseDurationOr(cfg.Catalog.HydrateTimeout, 20*time.Second)

	cat := catalog.New(catalog.Options{
		Logger:         log,
		HydrateTimeout: hydrateTimeout,
		Static:         cfg.Models,
		Sinks:          []catalog.Sink{catalog.NewDBSink(repo)},
	})

	routerService := gateway.NewServiceWithCatalog(log, repo, ingestor, cacheService, cat)
	analyticsService := analytics.NewService(repo)

	// Bootstrap providers (registers them with the catalog + gateway).
	gateway.BootstrapProviders(ctx, routerService, cfg.Providers, log)

	// Kick the first hydrate synchronously so `/api/v1/models` returns a
	// fully-formed list the moment the HTTP server starts accepting traffic.
	// Failures here are logged, not fatal — operators typically want the
	// gateway to stay up with the static YAML view rather than crash loop
	// when one provider is having a bad day.
	hydrateCtx, cancelHydrate := context.WithTimeout(ctx, hydrateTimeout+5*time.Second)
	if res, err := routerService.RefreshCatalog(hydrateCtx); err != nil {
		log.Warn("initial catalog hydrate failed", zap.Error(err))
	} else {
		log.Info("catalog ready",
			zap.Int("models", res.TotalModels),
			zap.Int("added", len(res.Added)),
			zap.Duration("duration", res.Duration),
		)
	}
	cancelHydrate()

	// Background rehydrate loop so upstream catalog drift (new models,
	// deprecations, pricing changes) propagates without a redeploy.
	if refreshInterval > 0 {
		watchCtx, cancelWatch := context.WithCancel(context.Background())
		defer cancelWatch()
		go cat.Watch(watchCtx, refreshInterval)
	}

	apiServer := server.New(cfg, log, repo, routerService, analyticsService, val)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: apiServer.Handler(),
	}

	// Start pprof server
	go func() {
		fmt.Println("CRITICAL: Starting pprof server on :6060")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			fmt.Printf("CRITICAL: pprof server failed: %v\n", err)
		}
	}()

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
		fmt.Println(cli.Gradient(line, ratio, cli.BrandBlue, cli.BrandPurple))
	}

	fmt.Println(cli.BoldText("\n          An exceptionally fast AI gateway."))

	fmt.Println()
	fmt.Printf("   Version:     %s\n", cli.BoldText(Version))
	fmt.Printf("   Go Version:  %s\n", runtime.Version())
	fmt.Printf("   Environment: %s\n", cli.BoldText(env))
	fmt.Printf("   Port:        %s\n", cli.BoldText(port))
	fmt.Printf("   Github:      %s\n", cli.BoldText("https://github.com/nulzo/prism"))
	fmt.Println("   --------------------------------------------------")
	fmt.Println()
}
