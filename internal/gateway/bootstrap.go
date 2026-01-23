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

		// validate provider configuration
		if err := validate.Struct(&pCfg); err != nil {
			log.Warn(fmt.Sprintf("%s %s %s",
				cli.CrossMark(),
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

		models, err := providerInstance.Models(ctx)

		if err != nil {
			msg := fmt.Sprintf("%s %s %s",
				cli.CrossMark(),
				cli.Stylize(pCfg.ID, cli.Red),
				cli.Stylize(fmt.Sprintf("(Failed: %v)", err), cli.Red),
			)
			log.Error(msg)
		}

		if len(models) == 0 {
			msg := fmt.Sprintf("%s %s %s",
				cli.CrossMark(),
				cli.Stylize(pCfg.ID, cli.Cyan),
				cli.Stylize("0 models found", cli.Red),
			)
			log.Warn(msg)
			continue
		}

		// perform health checks
		healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := providerInstance.Health(healthCtx); err != nil {
			cancel()
			log.Error("Provider unhealthy, skipping registration",
				zap.String("id", pCfg.ID),
				zap.Error(err))
			continue
		}
		cancel()

		// register with the service
		if err := service.RegisterProvider(ctx, providerInstance); err != nil {
			log.Error("Failed to register provider", zap.String("id", pCfg.ID), zap.Error(err))
			continue
		}

		msg := fmt.Sprintf("%s %s %s %s",
			cli.CheckMark(),
			cli.Stylize(fmt.Sprintf("%s\t", pCfg.ID), cli.Green),
			"registered with: ",
			cli.Stylize(fmt.Sprintf("%d models", len(models)), cli.White),
		)

		log.Info(msg)

		registeredCount++
	}

	if registeredCount == 0 {
		log.Warn("No providers were registered. API will not function correctly.")
	}

	return registeredCount
}
