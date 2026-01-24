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

	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/server"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"github.com/nulzo/model-router-api/internal/store/sqlite"
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

	printBanner(cfg.Server.Port, cfg.Server.Env)

	log, err := logger.New(logger.DefaultConfig())
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	logger.SetGlobal(log)
	defer log.Sync()

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
	defer repo.Close()

	routerService := gateway.NewService(log, repo, cacheService)
	analyticsService := analytics.NewService(repo)

	// Bootstrap providers
	ctx := context.Background()
	gateway.BootstrapProviders(ctx, routerService, cfg.Providers, log)

	apiServer := server.New(cfg, log, repo, routerService, analyticsService, val)

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
