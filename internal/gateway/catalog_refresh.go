package gateway

import (
	"context"
	"time"

	"github.com/nulzo/model-router-api/internal/catalog"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"go.uber.org/zap"
)

// CatalogRefresher is satisfied by gateway.Service.
type CatalogRefresher interface {
	RefreshCatalog(ctx context.Context, providerConfigs []config.ProviderConfig, hydrateTimeout time.Duration) error
}

// StartCatalogRefresh runs periodic catalog hydration when refresh_interval is
// configured. A zero or unset interval disables the background loop.
func StartCatalogRefresh(
	ctx context.Context,
	svc Service,
	providerConfigs []config.ProviderConfig,
	cfg config.CatalogConfig,
	log *zap.Logger,
) {
	interval := parseDuration(cfg.RefreshInterval, 0)
	if interval <= 0 {
		return
	}
	hydrateTimeout := parseDuration(cfg.HydrateTimeout, 30*time.Second)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := svc.RefreshCatalog(ctx, providerConfigs, hydrateTimeout); err != nil {
					log.Warn("catalog refresh failed", zap.Error(err))
				}
			}
		}
	}()
}

func parseDuration(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fallback
	}
	return d
}

// RefreshCatalog re-discovers models for every registered catalog-capable provider.
func (s *service) RefreshCatalog(
	ctx context.Context,
	providerConfigs []config.ProviderConfig,
	hydrateTimeout time.Duration,
) error {
	cfgByID := make(map[string]config.ProviderConfig, len(providerConfigs))
	for _, p := range providerConfigs {
		if p.Enabled {
			cfgByID[p.ID] = p
		}
	}

	s.mu.RLock()
	providers := make(map[string]llm.Provider, len(s.providers))
	for id, p := range s.providers {
		providers[id] = p
	}
	s.mu.RUnlock()

	for id, provider := range providers {
		pCfg, ok := cfgByID[id]
		if !ok {
			continue
		}
		models, err := catalog.ResolveProviderModels(ctx, provider, pCfg.StaticModels, pCfg, hydrateTimeout)
		if err != nil {
			return err
		}
		s.replaceProviderModels(id, models)
	}
	return nil
}
