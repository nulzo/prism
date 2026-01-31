CREATE TABLE request_usage_details (
    request_id TEXT PRIMARY KEY REFERENCES request_logs(id) ON DELETE CASCADE,
    
    -- Prompt Details
    prompt_tokens_cached INTEGER NOT NULL DEFAULT 0,
    prompt_tokens_cache_write INTEGER NOT NULL DEFAULT 0,
    prompt_tokens_audio INTEGER NOT NULL DEFAULT 0,
    prompt_tokens_video INTEGER NOT NULL DEFAULT 0,
    
    -- Completion Details
    completion_tokens_reasoning INTEGER NOT NULL DEFAULT 0,
    completion_tokens_image INTEGER NOT NULL DEFAULT 0,
    
    -- Cost Details (Stored in micros)
    cost_micros INTEGER, -- Nullable if not calculated/applicable
    is_byok BOOLEAN DEFAULT 0,
    
    upstream_cost_micros INTEGER, -- Total upstream cost (if BYOK)
    upstream_prompt_cost_micros INTEGER NOT NULL DEFAULT 0,
    upstream_completion_cost_micros INTEGER NOT NULL DEFAULT 0,
    
    -- Tool Usage
    web_search_requests INTEGER NOT NULL DEFAULT 0,
    
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
