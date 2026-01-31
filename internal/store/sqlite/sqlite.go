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
		upstream_model_id, upstream_remote_id, finish_reason,
		input_tokens, output_tokens, cached_tokens,
		latency_ms, ttft_ms, status_code, total_cost_micros, is_streamed,
		ip_address, user_agent, meta_json, created_at
	) VALUES (
		:id, :user_id, :api_key_id, :app_name, :provider_id, :model_id,
		:upstream_model_id, :upstream_remote_id, :finish_reason,
		:input_tokens, :output_tokens, :cached_tokens,
		:latency_ms, :ttft_ms, :status_code, :total_cost_micros, :is_streamed,
		:ip_address, :user_agent, :meta_json, :created_at
	)`
	if _, err := r.db.NamedExecContext(ctx, query, log); err != nil {
		return err
	}

	if log.UsageDetails != nil {
		log.UsageDetails.RequestID = log.ID
		queryDetails := `
		INSERT INTO request_usage_details (
			request_id,
			prompt_tokens_cached, prompt_tokens_cache_write, prompt_tokens_audio, prompt_tokens_video,
			completion_tokens_reasoning, completion_tokens_image,
			cost_micros, is_byok,
			upstream_cost_micros, upstream_prompt_cost_micros, upstream_completion_cost_micros,
			web_search_requests,
			created_at
		) VALUES (
			:request_id,
			:prompt_tokens_cached, :prompt_tokens_cache_write, :prompt_tokens_audio, :prompt_tokens_video,
			:completion_tokens_reasoning, :completion_tokens_image,
			:cost_micros, :is_byok,
			:upstream_cost_micros, :upstream_prompt_cost_micros, :upstream_completion_cost_micros,
			:web_search_requests,
			CURRENT_TIMESTAMP
		)`
		if _, err := r.db.NamedExecContext(ctx, queryDetails, log.UsageDetails); err != nil {
			// If details fail, we log it but don't fail the main log? 
			// Or return error? Returning error might cause ingestor retry loop to duplicate main log if it wasn't transactional.
			// Since we aren't in a guaranteed TX here, duplication risk exists on retry.
			return fmt.Errorf("failed to log usage details: %w", err)
		}
	}

	return nil
}

func (r *requestRepo) GetByID(ctx context.Context, id string) (*model.RequestLog, error) {
	var log model.RequestLog
	query := `SELECT * FROM request_logs WHERE id = ?`
	if err := r.db.GetContext(ctx, &log, query, id); err != nil {
		return nil, err
	}

	// Try to fetch usage details
	var details model.UsageDetails
	queryDetails := `SELECT * FROM request_usage_details WHERE request_id = ?`
	if err := r.db.GetContext(ctx, &details, queryDetails, id); err == nil {
		log.UsageDetails = &details
	} else if err != sql.ErrNoRows {
		// Log error but return partial data? Or fail?
		// For now, return what we have, maybe details aren't there.
	}

	return &log, nil
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

func (r *providerRepo) SyncModels(ctx context.Context, models []model.Model) error {
	// First, mark all as disabled. The loop below will re-enable present ones.
	if _, err := r.db.ExecContext(ctx, `UPDATE models SET is_enabled = 0`); err != nil {
		return err
	}

	query := `
	INSERT INTO models (
		id, provider_id, provider_model_id, is_enabled, is_public,
		input_cost_micros_per_1k, output_cost_micros_per_1k, context_window,
		created_at, updated_at
	) VALUES (
		:id, :provider_id, :provider_model_id, :is_enabled, :is_public,
		:input_cost_micros_per_1k, :output_cost_micros_per_1k, :context_window,
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
	)
	ON CONFLICT(id) DO UPDATE SET
		provider_id = excluded.provider_id,
		provider_model_id = excluded.provider_model_id,
		is_enabled = excluded.is_enabled,
		is_public = excluded.is_public,
		input_cost_micros_per_1k = excluded.input_cost_micros_per_1k,
		output_cost_micros_per_1k = excluded.output_cost_micros_per_1k,
		context_window = excluded.context_window,
		updated_at = CURRENT_TIMESTAMP`

	for _, m := range models {
		if _, err := r.db.NamedExecContext(ctx, query, m); err != nil {
			return err
		}
	}

	return nil
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
