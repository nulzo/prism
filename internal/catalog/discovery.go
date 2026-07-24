package catalog

import (
	"context"
	"strings"
	"time"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
)

// DiscoveryEnabled reports whether runtime model discovery should run for a
// provider. Catalog-capable providers default to enabled; set
// provider.config.discover_models to "false" or "0" to disable.
func DiscoveryEnabled(cfg config.ProviderConfig) bool {
	if v, ok := cfg.Config["discover_models"]; ok {
		v = strings.ToLower(strings.TrimSpace(v))
		return v != "false" && v != "0"
	}
	return true
}

// ResolveProviderModels returns the effective model list for a provider by
// merging static YAML entries with runtime discovery when supported. When
// discovery fails but static models exist, static models are returned so
// operators can still run with a curated fallback catalog.
func ResolveProviderModels(
	ctx context.Context,
	provider llm.Provider,
	static []api.ModelDefinition,
	cfg config.ProviderConfig,
	timeout time.Duration,
) ([]api.ModelDefinition, error) {
	catalogProvider, ok := provider.(llm.ModelCatalogProvider)
	if !ok || !DiscoveryEnabled(cfg) {
		return enabledOnly(static), nil
	}

	hydrateCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	discovered, err := catalogProvider.Models(hydrateCtx)
	if err != nil {
		if len(static) > 0 {
			return enabledOnly(static), nil
		}
		return nil, err
	}

	return Merge(static, discovered), nil
}

func enabledOnly(models []api.ModelDefinition) []api.ModelDefinition {
	out := make([]api.ModelDefinition, 0, len(models))
	for _, m := range models {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}
