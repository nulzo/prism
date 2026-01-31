package store

import (
	"context"

	"github.com/nulzo/model-router-api/internal/store/model"
)

type contextKey string

const (
	ContextKeyAPIKey  contextKey = "api_key"
	ContextKeyAppName contextKey = "app_name"
)

// Repository is the main contract for the data layer.
// It exposes sub-repositories for specific domains.
type Repository interface {
	APIKeys() APIKeyRepository
	Requests() RequestRepository
	Providers() ProviderRepository
	Users() UserRepository

	// transaction support
	WithTx(ctx context.Context, fn func(repo Repository) error) error

	Close() error
}

type APIKeyRepository interface {
	// GetByHash retrieves a key by its hashed value (for auth).
	GetByHash(ctx context.Context, hash string) (*model.APIKey, error)
	// Create issues a new API key.
	Create(ctx context.Context, key *model.APIKey) error
	// UpdateUsage increments usage stats.
	UpdateUsage(ctx context.Context, id string) error
	// ListByUserID returns all keys for a user.
	ListByUserID(ctx context.Context, userID string) ([]model.APIKey, error)
}

type RequestRepository interface {
	// Log stores a completed request.
	Log(ctx context.Context, log *model.RequestLog) error
	// GetByID returns a single request log by ID, including usage details if available.
	GetByID(ctx context.Context, id string) (*model.RequestLog, error)
	// GetRecent returns the last N logs for a user.
	GetRecent(ctx context.Context, userID string, limit int) ([]model.RequestLog, error)
	// GetDailyStats returns aggregated stats grouped by day.
	GetDailyStats(ctx context.Context, days int) ([]model.DailyStats, error)
}

type ProviderRepository interface {
	// ListActive returns all enabled providers and their models.
	ListActive(ctx context.Context) ([]model.Provider, error)
	// GetModelPricing retrieves pricing for cost calculation.
	GetModelPricing(ctx context.Context, modelID string) (*model.Model, error)
	// SyncModels syncs the models from the configuration to the database.
	SyncModels(ctx context.Context, models []model.Model) error
}

type UserRepository interface {
	Get(ctx context.Context, id string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	GetWallet(ctx context.Context, userID string) (*model.Wallet, error)
}
