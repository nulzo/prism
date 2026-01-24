-- Enable Foreign Keys
PRAGMA foreign_keys = ON;

-- Users: The owners of keys and wallets
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user', -- 'admin', 'user'
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Wallets: Manages credits and billing. 
-- We store balance in "micros" (1/1,000,000 USD) to avoid floating point errors.
CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    balance_micros INTEGER NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'USD',
    is_frozen BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- API Keys: The authentication mechanism
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    wallet_id TEXT REFERENCES wallets(id), -- Optional: Link key to specific wallet
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE, -- SHA-256 hash of the key
    key_prefix TEXT NOT NULL, -- The first 8 chars for display (e.g., "sk-prod-")
    scopes TEXT NOT NULL DEFAULT '', -- JSON array of permissions
    expires_at DATETIME,
    last_used_at DATETIME,
    monthly_limit_micros INTEGER, -- NULL means unlimited
    is_active BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_user ON api_keys(user_id);

-- Providers: Upstream LLM providers (Dynamic Configuration)
CREATE TABLE providers (
    id TEXT PRIMARY KEY, -- e.g., 'openai', 'anthropic'
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key_enc TEXT, -- Encrypted API key (application level encryption)
    config_json TEXT NOT NULL DEFAULT '{}', -- Extra config like org_id
    is_enabled BOOLEAN NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 0, -- Higher number = higher priority
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Models: Pricing and mapping for specific models
CREATE TABLE models (
    id TEXT PRIMARY KEY, -- e.g., 'gpt-4-turbo' (Global ID)
    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    provider_model_id TEXT NOT NULL, -- The actual ID sent to provider (e.g. 'gpt-4-0125-preview')
    is_enabled BOOLEAN NOT NULL DEFAULT 1,
    is_public BOOLEAN NOT NULL DEFAULT 1, -- Can users see this?
    input_cost_micros_per_1k INTEGER NOT NULL DEFAULT 0,
    output_cost_micros_per_1k INTEGER NOT NULL DEFAULT 0,
    context_window INTEGER NOT NULL DEFAULT 4096,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider_id, provider_model_id)
);

-- Request Logs: The Ledger. High volume.
CREATE TABLE request_logs (
    id TEXT PRIMARY KEY, -- Request ID
    user_id TEXT NOT NULL, -- Denormalized for query speed, but technically FK
    api_key_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    
    -- Usage Stats
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    
    -- Performance Stats
    latency_ms INTEGER NOT NULL DEFAULT 0,
    ttft_ms INTEGER, -- Time to first token
    status_code INTEGER NOT NULL,
    
    -- Cost Stats (Calculated at end of request)
    total_cost_micros INTEGER NOT NULL DEFAULT 0,
    
    -- Metadata
    ip_address TEXT,
    user_agent TEXT,
    meta_json TEXT, -- Arbitrary tags
    
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_logs_user_date ON request_logs(user_id, created_at);
CREATE INDEX idx_logs_api_key_date ON request_logs(api_key_id, created_at);
CREATE INDEX idx_logs_created_at ON request_logs(created_at);

-- Audit Events: Sensitive actions (creating keys, changing wallet balance)
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_user_id TEXT NOT NULL, -- Who did it
    target_resource TEXT NOT NULL, -- e.g. "wallet:123"
    action TEXT NOT NULL, -- "credit_added"
    details_json TEXT NOT NULL,
    ip_address TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
