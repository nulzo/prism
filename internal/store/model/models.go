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
	KeyHash            string         `db:"key_hash" json:"-"`            // Never return hash
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
	ID                    string    `db:"id" json:"id"`
	ProviderID            string    `db:"provider_id" json:"provider_id"`
	ProviderModelID       string    `db:"provider_model_id" json:"provider_model_id"`
	IsEnabled             bool      `db:"is_enabled" json:"is_enabled"`
	IsPublic              bool      `db:"is_public" json:"is_public"`
	InputCostMicrosPer1k  int64     `db:"input_cost_micros_per_1k" json:"input_cost_micros_per_1k"`
	OutputCostMicrosPer1k int64     `db:"output_cost_micros_per_1k" json:"output_cost_micros_per_1k"`
	ContextWindow         int       `db:"context_window" json:"context_window"`
	CreatedAt             time.Time `db:"created_at" json:"created_at"`
	UpdatedAt             time.Time `db:"updated_at" json:"updated_at"`
}

// RequestLog captures the full detail of a completed inference request.
type RequestLog struct {
	ID              string        `db:"id" json:"id"`
	UserID          string        `db:"user_id" json:"user_id"`
	APIKeyID        string        `db:"api_key_id" json:"api_key_id"`
	AppName         string        `db:"app_name" json:"app_name"`
	ProviderID      string        `db:"provider_id" json:"provider_id"`
	ModelID         string        `db:"model_id" json:"model_id"`
	UpstreamModelID string        `db:"upstream_model_id" json:"upstream_model_id"`
	UpstreamRemoteID string       `db:"upstream_remote_id" json:"upstream_remote_id"`
	FinishReason    string        `db:"finish_reason" json:"finish_reason"`
	InputTokens     int           `db:"input_tokens" json:"input_tokens"`
	OutputTokens    int           `db:"output_tokens" json:"output_tokens"`
	CachedTokens    int           `db:"cached_tokens" json:"cached_tokens"`
	LatencyMS       int64         `db:"latency_ms" json:"latency_ms"`
	TTFTMS          sql.NullInt64 `db:"ttft_ms" json:"ttft_ms,omitempty"`
	StatusCode      int           `db:"status_code" json:"status_code"`
	TotalCostMicros int64         `db:"total_cost_micros" json:"total_cost_micros"`
	IsStreamed      bool          `db:"is_streamed" json:"is_streamed"`
	IPAddress       string        `db:"ip_address" json:"ip_address"`
	UserAgent       string        `db:"user_agent" json:"user_agent"`
	MetaJSON        string        `db:"meta_json" json:"meta_json"`
	CreatedAt       time.Time     `db:"created_at" json:"created_at"`

	// Detailed Usage (Joined but not in request_logs table)
	UsageDetails *UsageDetails `db:"-" json:"usage_details,omitempty"`
}

type UsageDetails struct {
	RequestID string `db:"request_id" json:"request_id"`

	PromptTokensCached     int `db:"prompt_tokens_cached" json:"prompt_tokens_cached"`
	PromptTokensCacheWrite int `db:"prompt_tokens_cache_write" json:"prompt_tokens_cache_write"`
	PromptTokensAudio      int `db:"prompt_tokens_audio" json:"prompt_tokens_audio"`
	PromptTokensVideo      int `db:"prompt_tokens_video" json:"prompt_tokens_video"`

	CompletionTokensReasoning int `db:"completion_tokens_reasoning" json:"completion_tokens_reasoning"`
	CompletionTokensImage     int `db:"completion_tokens_image" json:"completion_tokens_image"`

	CostMicros *int64 `db:"cost_micros" json:"cost_micros,omitempty"`
	IsBYOK     bool   `db:"is_byok" json:"is_byok"`

	UpstreamCostMicros           *int64 `db:"upstream_cost_micros" json:"upstream_cost_micros,omitempty"`
	UpstreamPromptCostMicros     int64  `db:"upstream_prompt_cost_micros" json:"upstream_prompt_cost_micros"`
	UpstreamCompletionCostMicros int64  `db:"upstream_completion_cost_micros" json:"upstream_completion_cost_micros"`

	WebSearchRequests int `db:"web_search_requests" json:"web_search_requests"`
}

// AuditEvent represents a security or critical system event.
type AuditEvent struct {
	ID             string    `db:"id" json:"id"`
	ActorUserID    string    `db:"actor_user_id" json:"actor_user_id"`
	TargetResource string    `db:"target_resource" json:"target_resource"`
	Action         string    `db:"action" json:"action"`
	DetailsJSON    string    `db:"details_json" json:"details_json"`
	IPAddress      string    `db:"ip_address" json:"ip_address,omitempty"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
}

// DailyStats represents aggregated usage data for a specific day.
type DailyStats struct {

	Date            string  `db:"date" json:"date"`
	TotalRequests   int     `db:"total_requests" json:"total_requests"`
	TotalTokens     int     `db:"total_tokens" json:"total_tokens"`
	TotalCostMicros int64   `db:"total_cost_micros" json:"total_cost_micros"`
	AverageLatency  float64 `db:"avg_latency" json:"avg_latency"`
}
