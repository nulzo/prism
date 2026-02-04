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
	"strconv"

	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/server"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/internal/store/sqlite"
	"go.uber.org/zap"

	_ "github.com/nulzo/model-router-api/internal/llm/anthropic"
	_ "github.com/nulzo/model-router-api/internal/llm/bfl"
	_ "github.com/nulzo/model-router-api/internal/llm/google"
	_ "github.com/nulzo/model-router-api/internal/llm/ollama"
	_ "github.com/nulzo/model-router-api/internal/llm/openai"
	_ "expvar"
	_ "net/http/pprof"
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

func parseCost(costStr string) int64 {
	if costStr == "" {
		return 0
	}
	val, err := strconv.ParseFloat(costStr, 64)
	if err != nil {
		return 0
	}
	// Logic: Input is Dollars per 1M tokens. Output is Micros per 1K tokens.
	// Micros per 1K = (DollarsPer1M * 1,000,000) / 1000 = DollarsPer1M * 1000.
	return int64(val * 1000)
}

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

	// Sync models to DB
	ctx := context.Background()
	if err := repo.WithTx(ctx, func(r store.Repository) error {
		// Sync Providers first
		var dbProviders []model.Provider
		for _, p := range cfg.Providers {
			// Encrypt API key? For now, we store as is or placeholder if config is source of truth.
			// Since we load from config on every boot, we might just store "CONFIGURED" or similar to avoid saving secrets in plaintext DB if that's a concern.
			// However, for functionality, if we want to move to dynamic config later, we'd need the real key.
			// Assuming local SQLite is secured or we trust the env vars.
			// Let's store a masked version or just empty if we rely on config-loaded instances.
			// Actually, the Service uses the IN-MEMORY providers loaded from config.
			// This DB sync is mainly for "Reporting" and "Audit" purposes so we know what providers existed.
			
			dbP := model.Provider{
				ID:         p.ID,
				Name:       p.ID, // Or mapped name
				BaseURL:    "config", // We don't have base URL handy in the simple config struct sometimes?
				IsEnabled:  p.Enabled,
				Priority:   0, // Config doesn't specify priority explicitly usually?
				ConfigJSON: "{}", 
			}
			dbProviders = append(dbProviders, dbP)
		}
		if err := r.Providers().SyncProviders(ctx, dbProviders); err != nil {
			return err
		}

		var dbModels []model.Model
		for _, m := range cfg.Models {
			// Ensure upstream ID is set
			upstreamID := m.UpstreamID
			if upstreamID == "" {
				// Fallback if needed, or maybe it is part of ID?
				// Usually upstream_id is required in config.
				upstreamID = m.ID
			}

			dbM := model.Model{
				ID:                    m.ID,
				ProviderID:            m.ProviderID,
				ProviderModelID:       upstreamID,
				IsEnabled:             m.Enabled,
				IsPublic:              true, // Default to true as config implies availability
				InputCostMicrosPer1k:  parseCost(m.Pricing.Prompt),
				OutputCostMicrosPer1k: parseCost(m.Pricing.Completion),
				ContextWindow:         m.ContextLength,
			}
			dbModels = append(dbModels, dbM)
		}
		return r.Providers().SyncModels(ctx, dbModels)
	}); err != nil {
		logger.Fatal("Failed to sync models", zap.Error(err))
	}

	// Initialize Analytics Ingestor
	ingestor := analytics.NewIngestor(log, repo)
	ingestor.Start(context.Background())
	defer ingestor.Stop()

	routerService := gateway.NewService(log, repo, ingestor, cacheService)
	analyticsService := analytics.NewService(repo)

	// Bootstrap providers
	gateway.BootstrapProviders(ctx, routerService, cfg.Providers, log)

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
