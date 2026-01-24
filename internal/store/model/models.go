package model

import (
	"database/sql"
	"time"
)

// User represents a tenant or individual developer.
type User struct {
	ID        string    `db:"id" json:"id"`
	Email     string    `db:"email" json:"email"`
	Name      string    `db:"name" json:"name"`
	Role      string    `db:"role" json:"role"` // 'admin', 'user'
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Wallet tracks the financial balance of a user.
type Wallet struct {
	ID            string    `db:"id" json:"id"`
	UserID        string    `db:"user_id" json:"user_id"`
	BalanceMicros int64     `db:"balance_micros" json:"balance_micros"`
	Currency      string    `db:"currency" json:"currency"`
	IsFrozen      bool      `db:"is_frozen" json:"is_frozen"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at" json:"updated_at"`
}

// APIKey is the credential used to access the API.
type APIKey struct {
	ID                 string         `db:"id" json:"id"`
	UserID             string         `db:"user_id" json:"user_id"`
	WalletID           sql.NullString `db:"wallet_id" json:"wallet_id,omitempty"`
	Name               string         `db:"name" json:"name"`
	KeyHash            string         `db:"key_hash" json:"-"`          // Never return hash
	KeyPrefix          string         `db:"key_prefix" json:"key_prefix"` // Display only
	Scopes             string         `db:"scopes" json:"scopes"`         // JSON array
	ExpiresAt          sql.NullTime   `db:"expires_at" json:"expires_at,omitempty"`
	LastUsedAt         sql.NullTime   `db:"last_used_at" json:"last_used_at,omitempty"`
	MonthlyLimitMicros sql.NullInt64  `db:"monthly_limit_micros" json:"monthly_limit_micros,omitempty"`
	IsActive           bool           `db:"is_active" json:"is_active"`
	CreatedAt          time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time      `db:"updated_at" json:"updated_at"`
}

// Provider represents an upstream LLM service (OpenAI, Anthropic).
type Provider struct {
	ID         string    `db:"id" json:"id"`
	Name       string    `db:"name" json:"name"`
	BaseURL    string    `db:"base_url" json:"base_url"`
	APIKeyEnc  string    `db:"api_key_enc" json:"-"` // Encrypted
	ConfigJSON string    `db:"config_json" json:"config_json"`
	IsEnabled  bool      `db:"is_enabled" json:"is_enabled"`
	Priority   int       `db:"priority" json:"priority"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// Model represents a specific model offered by a provider with pricing.
type Model struct {
	ID                     string    `db:"id" json:"id"`
	ProviderID             string    `db:"provider_id" json:"provider_id"`
	ProviderModelID        string    `db:"provider_model_id" json:"provider_model_id"`
	IsEnabled              bool      `db:"is_enabled" json:"is_enabled"`
	IsPublic               bool      `db:"is_public" json:"is_public"`
	InputCostMicrosPer1k   int64     `db:"input_cost_micros_per_1k" json:"input_cost_micros_per_1k"`
	OutputCostMicrosPer1k  int64     `db:"output_cost_micros_per_1k" json:"output_cost_micros_per_1k"`
	ContextWindow          int       `db:"context_window" json:"context_window"`
	CreatedAt              time.Time `db:"created_at" json:"created_at"`
	UpdatedAt              time.Time `db:"updated_at" json:"updated_at"`
}

// RequestLog captures the full detail of a completed inference request.
type RequestLog struct {
	ID              string         `db:"id" json:"id"`
	UserID          string         `db:"user_id" json:"user_id"`
	APIKeyID        string         `db:"api_key_id" json:"api_key_id"`
	ProviderID      string         `db:"provider_id" json:"provider_id"`
	ModelID         string         `db:"model_id" json:"model_id"`
	InputTokens     int            `db:"input_tokens" json:"input_tokens"`
	OutputTokens    int            `db:"output_tokens" json:"output_tokens"`
	CachedTokens    int            `db:"cached_tokens" json:"cached_tokens"`
	LatencyMS       int64          `db:"latency_ms" json:"latency_ms"`
	TTFTMS          sql.NullInt64  `db:"ttft_ms" json:"ttft_ms,omitempty"`
	StatusCode      int            `db:"status_code" json:"status_code"`
	TotalCostMicros int64          `db:"total_cost_micros" json:"total_cost_micros"`
	IPAddress       string         `db:"ip_address" json:"ip_address"`
	UserAgent       string         `db:"user_agent" json:"user_agent"`
	MetaJSON        string         `db:"meta_json" json:"meta_json"`
	CreatedAt       time.Time      `db:"created_at" json:"created_at"`
}

// DailyStats represents aggregated usage data for a specific day.
type DailyStats struct {
	Date            string `db:"date" json:"date"`
	TotalRequests   int    `db:"total_requests" json:"total_requests"`
	TotalTokens     int    `db:"total_tokens" json:"total_tokens"`
	TotalCostMicros int64  `db:"total_cost_micros" json:"total_cost_micros"`
	AverageLatency  float64 `db:"avg_latency" json:"avg_latency"`
}
