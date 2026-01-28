package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/internal/store/model"
)

// DB defines the interface for database operations (satisfied by *sqlx.DB and *sqlx.Tx)
type DB interface {
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// SqliteRepository implements store.Repository
type SqliteRepository struct {
	db       *sqlx.DB // Required for starting new transactions
	executor DB       // Used for actual queries (can be *sqlx.DB or *sqlx.Tx)
}

func NewSqliteRepository(db *sqlx.DB) *SqliteRepository {
	return &SqliteRepository{
		db:       db,
		executor: db,
	}
}

func (r *SqliteRepository) Close() error {
	return r.db.Close()
}

func (r *SqliteRepository) WithTx(ctx context.Context, fn func(repo store.Repository) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	// Create a repository instance that uses the transaction
	txRepo := &SqliteRepository{
		db:       r.db, // Keep the original DB handle
		executor: tx,
	}

	if err := fn(txRepo); err != nil {
		// attempt rollback, but prioritize original error
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *SqliteRepository) APIKeys() store.APIKeyRepository {
	return &apiKeyRepo{db: r.executor}
}

func (r *SqliteRepository) Requests() store.RequestRepository {
	return &requestRepo{db: r.executor}
}

func (r *SqliteRepository) Providers() store.ProviderRepository {
	return &providerRepo{db: r.executor}
}

func (r *SqliteRepository) Users() store.UserRepository {
	return &userRepo{db: r.executor}
}

type apiKeyRepo struct {
	db DB
}

func (r *apiKeyRepo) GetByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	var key model.APIKey
	// active check is part of the query for speed
	query := `SELECT * FROM api_keys WHERE key_hash = ? AND is_active = 1`
	err := r.db.GetContext(ctx, &key, query, hash)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *apiKeyRepo) Create(ctx context.Context, key *model.APIKey) error {
	query := `
	INSERT INTO api_keys (id, user_id, wallet_id, name, key_hash, key_prefix, scopes, created_at, updated_at)
	VALUES (:id, :user_id, :wallet_id, :name, :key_hash, :key_prefix, :scopes, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, key)
	return err
}

func (r *apiKeyRepo) UpdateUsage(ctx context.Context, id string) error {
	query := `UPDATE api_keys SET last_used_at = ? WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

func (r *apiKeyRepo) ListByUserID(ctx context.Context, userID string) ([]model.APIKey, error) {
	var keys []model.APIKey
	err := r.db.SelectContext(ctx, &keys, `SELECT * FROM api_keys WHERE user_id = ?`, userID)
	return keys, err
}

type requestRepo struct {
	db DB
}

func (r *requestRepo) Log(ctx context.Context, log *model.RequestLog) error {
	// Using NamedExec for cleaner mapping
	query := `
	INSERT INTO request_logs (
		id, user_id, api_key_id, app_name, provider_id, model_id,
		input_tokens, output_tokens, cached_tokens,
		latency_ms, ttft_ms, status_code, total_cost_micros,
		ip_address, user_agent, meta_json, created_at
	) VALUES (
		:id, :user_id, :api_key_id, :app_name, :provider_id, :model_id,
		:input_tokens, :output_tokens, :cached_tokens,
		:latency_ms, :ttft_ms, :status_code, :total_cost_micros,
		:ip_address, :user_agent, :meta_json, :created_at
	)`
	_, err := r.db.NamedExecContext(ctx, query, log)
	return err
}

func (r *requestRepo) GetRecent(ctx context.Context, userID string, limit int) ([]model.RequestLog, error) {
	var logs []model.RequestLog
	query := `SELECT * FROM request_logs WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`
	err := r.db.SelectContext(ctx, &logs, query, userID, limit)
	return logs, err
}

func (r *requestRepo) GetDailyStats(ctx context.Context, days int) ([]model.DailyStats, error) {
	var stats []model.DailyStats
	query := `
		SELECT 
			DATE(created_at) as date,
			COUNT(*) as total_requests,
			SUM(input_tokens + output_tokens) as total_tokens,
			SUM(total_cost_micros) as total_cost_micros,
			AVG(latency_ms) as avg_latency
		FROM request_logs 
		WHERE created_at >= DATE('now', ?)
		GROUP BY date
		ORDER BY date DESC
	`
	// SQLite date offset format is '-7 days'
	err := r.db.SelectContext(ctx, &stats, query, fmt.Sprintf("-%d days", days))
	return stats, err
}

type providerRepo struct {
	db DB
}

func (r *providerRepo) ListActive(ctx context.Context) ([]model.Provider, error) {
	var providers []model.Provider
	err := r.db.SelectContext(ctx, &providers, `SELECT * FROM providers WHERE is_enabled = 1 ORDER BY priority DESC`)
	return providers, err
}

func (r *providerRepo) GetModelPricing(ctx context.Context, modelID string) (*model.Model, error) {
	var m model.Model
	err := r.db.GetContext(ctx, &m, `SELECT * FROM models WHERE id = ?`, modelID)
	return &m, err
}

type userRepo struct {
	db DB
}

func (r *userRepo) Get(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE id = ?`, id)
	return &u, err
}

func (r *userRepo) Create(ctx context.Context, user *model.User) error {
	query := `
	INSERT INTO users (id, email, name, role, created_at, updated_at)
	VALUES (:id, :email, :name, :role, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, user)
	return err
}

func (r *userRepo) GetWallet(ctx context.Context, userID string) (*model.Wallet, error) {
	var w model.Wallet
	err := r.db.GetContext(ctx, &w, `SELECT * FROM wallets WHERE user_id = ?`, userID)
	return &w, err
}
