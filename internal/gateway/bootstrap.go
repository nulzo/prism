package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"go.uber.org/zap"
)

// BootstrapProviders initializes and registers all enabled providers from configuration.
func BootstrapProviders(ctx context.Context, service Service, providers []config.ProviderConfig, log *zap.Logger) int {
	registeredCount := 0
	validate := validator.New()

	for _, pCfg := range providers {
		if !pCfg.Enabled {
			continue
		}

		// Validate provider configuration individually
		if err := validate.Struct(&pCfg); err != nil {
			log.Warn(fmt.Sprintf("%s %s %s",
				cli.WarningSign(),
				cli.Stylize(fmt.Sprintf("%s\t", pCfg.ID), cli.Black),
				cli.Stylize("Skipping provider due to missing API key", cli.Yellow),
			))
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

		// Perform Health Check
		healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := providerInstance.Health(healthCtx); err != nil {
			cancel()
			log.Error("Provider unhealthy, skipping registration",
				zap.String("id", pCfg.ID),
				zap.Error(err))
			continue
		}
		cancel()

		// Register with the Service
		if err := service.RegisterProvider(ctx, providerInstance); err != nil {
			log.Error("Failed to register provider", zap.String("id", pCfg.ID), zap.Error(err))
			continue
		}

		registeredCount++
	}

	if registeredCount == 0 {
		log.Warn("No providers were registered. API will not function correctly.")
	}

	return registeredCount
}
