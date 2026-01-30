package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/server"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store/cache"
	"github.com/nulzo/model-router-api/internal/store/sqlite"
	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Provider ---

type MockProvider struct {
	ID             string
	MockChatResp   *api.ChatResponse
	MockStreamResp []api.StreamResult
	MockModels     []api.ModelDefinition
	Called         bool
}

func (m *MockProvider) Name() string { return m.ID }
func (m *MockProvider) Type() string { return "mock" }
func (m *MockProvider) Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error) {
	m.Called = true
	if m.MockChatResp != nil {
		return m.MockChatResp, nil
	}
	return &api.ChatResponse{
		ID:     "mock-id",
		Object: "chat.completion",
		Choices: []api.Choice{
			{Message: &api.ChatMessage{Role: "assistant", Content: api.Content{Text: "Mock Response"}}},
		},
	}, nil
}
func (m *MockProvider) Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error) {
	ch := make(chan api.StreamResult)
	go func() {
		defer close(ch)
		for _, r := range m.MockStreamResp {
			ch <- r
		}
	}()
	return ch, nil
}
func (m *MockProvider) Models(ctx context.Context) ([]api.ModelDefinition, error) {
	return m.MockModels, nil
}
func (m *MockProvider) Health(ctx context.Context) error { return nil }

// --- Test Setup ---

func setupTestServer(t *testing.T) (*httptest.Server, *MockProvider) {
	// 1. Logger
	log, _ := logger.New(logger.DefaultConfig())
	logger.SetGlobal(log)

	// 2. In-Memory DB
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)

	// Init Schema
	schema := `
	CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE wallets (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		balance_micros INTEGER NOT NULL DEFAULT 0,
		currency TEXT NOT NULL DEFAULT 'USD',
		is_frozen BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		wallet_id TEXT,
		name TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		key_prefix TEXT NOT NULL,
		scopes TEXT NOT NULL DEFAULT '',
		expires_at DATETIME,
		last_used_at DATETIME,
		monthly_limit_micros INTEGER,
		is_active BOOLEAN NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE providers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key_enc TEXT,
		config_json TEXT NOT NULL DEFAULT '{}',
		is_enabled BOOLEAN NOT NULL DEFAULT 1,
		priority INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE models (
		id TEXT PRIMARY KEY,
		provider_id TEXT NOT NULL,
		provider_model_id TEXT NOT NULL,
		is_enabled BOOLEAN NOT NULL DEFAULT 1,
		is_public BOOLEAN NOT NULL DEFAULT 1,
		input_cost_micros_per_1k INTEGER NOT NULL DEFAULT 0,
		output_cost_micros_per_1k INTEGER NOT NULL DEFAULT 0,
		context_window INTEGER NOT NULL DEFAULT 4096,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(provider_id, provider_model_id)
	);
	CREATE TABLE request_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		api_key_id TEXT NOT NULL,
		app_name TEXT,
		provider_id TEXT NOT NULL,
		model_id TEXT NOT NULL,
		upstream_model_id TEXT,
		upstream_remote_id TEXT,
		finish_reason TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		ttft_ms INTEGER,
		status_code INTEGER NOT NULL,
		total_cost_micros INTEGER NOT NULL DEFAULT 0,
		ip_address TEXT,
		user_agent TEXT,
		meta_json TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = db.Exec(schema)
	require.NoError(t, err)

	repo := sqlite.NewSqliteRepository(db)

	// 3. Components
	cacheSvc := cache.NewMemoryCache()
	
	// Create Ingestor for analytics (required by Gateway service)
	ingestor := analytics.NewIngestor(log, repo)
	ingestor.Start(context.Background())
	
	routerSvc := gateway.NewService(log, repo, ingestor, cacheSvc)
	analyticsSvc := analytics.NewService(repo)
	val := validator.New()

	// 4. Config
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:        0, // Random port
			Env:         "development", // Must be development, production, or staging
			AuthEnabled: false, // Disable auth for easy testing
		},
		RateLimit: config.RateLimitConfig{
			RequestsPerSecond: 100,
			Burst:             100,
		},
		Redis: config.RedisConfig{
			Enabled: false,
		},
		Database: config.DatabaseConfig{
			Path: ":memory:",
		},
	}

	// 5. Register Mock Provider
	mockP := &MockProvider{
		ID: "mock-provider",
		MockModels: []api.ModelDefinition{
			{ID: "test-model", ProviderID: "mock-provider", UpstreamID: "mock-model", Enabled: true},
		},
	}
	err = routerSvc.RegisterProvider(context.Background(), mockP)
	require.NoError(t, err)

	// 6. Server
	srv := server.New(cfg, log, repo, routerSvc, analyticsSvc, val)
	ts := httptest.NewServer(srv.Handler())

	return ts, mockP
}

// helper to make requests against test server
func makeRequest(t *testing.T, ts *httptest.Server, method, path string, payload interface{}, target interface{}) int {
	var body io.Reader
	if payload != nil {
		jsonBytes, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequest(method, ts.URL+path, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := ts.Client()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if target != nil {
		// If we expect JSON but get something else, dump the body for debugging
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// If status is not 200/201, it might be an error response
		// Try to unmarshal into target
		if len(b) > 0 {
			if err := json.Unmarshal(b, target); err != nil {
				// If strictly required, fail. But sometimes we check status code first.
				// Let's log if it fails.
				t.Logf("Failed to decode response: %s", string(b))
			}
		}
	}

	return resp.StatusCode
}

func TestHealthCheck(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestListModels(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	var result struct {
		Object string      `json:"object"`
		Data   []api.Model `json:"data"`
	}

	code := makeRequest(t, ts, "GET", "/api/v1/models", nil, &result)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "list", result.Object)
	assert.NotEmpty(t, result.Data, "Models list should not be empty")
	assert.Equal(t, "test-model", result.Data[0].ID)
}

func TestChatCompletion_Sync(t *testing.T) {
	ts, mockP := setupTestServer(t)
	defer ts.Close()

	req := api.ChatRequest{
		Model:    "test-model",
		Messages: []api.ChatMessage{{Role: "user", Content: api.Content{Text: "Say hi"}}},
		Stream:   false,
	}

	var resp api.ChatResponse
	code := makeRequest(t, ts, "POST", "/api/v1/chat/completions", req, &resp)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "chat.completion", resp.Object)
	require.NotEmpty(t, resp.Choices)
	assert.Equal(t, "Mock Response", resp.Choices[0].Message.Content.Text)
	assert.True(t, mockP.Called, "Mock provider should have been called")
}

func TestValidationError(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()

	// purposefully bad payload (missing Model, invalid Role)
	payload := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "bad_role", "content": "hello"},
		},
	}

	// capture generic map to check error fields
	var errResp map[string]interface{}
	code := makeRequest(t, ts, "POST", "/api/v1/chat/completions", payload, &errResp)

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "Validation Error", errResp["title"])

	// check the RFC 9457 "errors" extension
	errors, ok := errResp["errors"].(map[string]interface{})
	require.True(t, ok, "Should contain 'errors' map")

	assert.Contains(t, errors, "messages[0].role")
	assert.Contains(t, errors, "model")
}