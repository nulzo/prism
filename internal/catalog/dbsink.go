package catalog

import (
	"context"
	"strconv"

	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/model"
)

// DBSink persists the catalog snapshot to the SQL `models` table so
// request-time pricing lookups (repo.Providers().GetModelPricing) resolve
// for every entry — including upstream-discovered models that never lived
// in YAML. Previously the DB was seeded once at startup from YAML only,
// which meant provider-announced models were routable but had no pricing
// row, so their usage cost was silently dropped.
type DBSink struct {
	repo store.Repository
}

// NewDBSink wires a SQL-backed sink. The sink writes on every successful
// hydrate; skipping it is equivalent to running the old startup-only
// SyncModels call with a stale YAML snapshot.
func NewDBSink(repo store.Repository) *DBSink { return &DBSink{repo: repo} }

func (s *DBSink) SinkName() string { return "sqlite-models" }

func (s *DBSink) Absorb(ctx context.Context, snap Snapshot) error {
	rows := make([]model.Model, 0, len(snap.Entries))
	for _, e := range snap.Entries {
		upstream := e.UpstreamID
		if upstream == "" {
			upstream = e.ID
		}
		rows = append(rows, model.Model{
			ID:                    e.ID,
			ProviderID:            e.ProviderID,
			ProviderModelID:       upstream,
			IsEnabled:             e.Enabled,
			IsPublic:              true,
			InputCostMicrosPer1k:  parseCost(e.Pricing.Prompt),
			OutputCostMicrosPer1k: parseCost(e.Pricing.Completion),
			ContextWindow:         e.ContextLength,
		})
	}
	// Run in its own transaction so a partial write can't leave the DB
	// in a half-baked state if the process is killed mid-sync.
	return s.repo.WithTx(ctx, func(r store.Repository) error {
		return r.Providers().SyncModels(ctx, rows)
	})
}

// parseCost mirrors the conversion logic in cmd/server/main.go: pricing
// strings in YAML are dollars per 1M tokens; the DB column is micros per
// 1k tokens. micros_per_1k = dollars_per_1M * 1000.
func parseCost(costStr string) int64 {
	if costStr == "" {
		return 0
	}
	v, err := strconv.ParseFloat(costStr, 64)
	if err != nil {
		return 0
	}
	return int64(v * 1000)
}
