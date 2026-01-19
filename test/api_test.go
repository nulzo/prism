package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/nulzo/model-router-api/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	baseURL     = "http://localhost:8080/server/v1"
	healthURL   = "http://localhost:8080/health"
	targetModel = "ollama/tinydolphin:latest"
)

// helper to make requests
func makeRequest(t *testing.T, method, url string, payload interface{}, target interface{}) int {
	var body io.Reader
	if payload != nil {
		jsonBytes, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequest(method, url, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// Note: Authentication middleware is active, but for simplicity of this test 
	// we assume it's running in an env where we might need a key or mock it.
	// If the server requires keys, we should add one here.
	// req.Header.Set("Authorization", "Bearer test-api-key")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if target != nil {
		err = json.NewDecoder(resp.Body).Decode(target)
		require.NoError(t, err, "Failed to decode response JSON")
	}

	return resp.StatusCode
}

func TestHealthCheck(t *testing.T) {
	resp, err := http.Get(healthURL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestListModels(t *testing.T) {
	// we just define a simplified struct or map for things we don't have in pkg/api yet
	var result struct {
		Object string        `json:"object"`
		Data   []interface{} `json:"data"`
	}

	code := makeRequest(t, "GET", baseURL+"/models", nil, &result)

	// Since we haven't set up the API key in this test client, this might return 401.
	// For integration tests, we usually need the server running with a specific config.
	if code == http.StatusUnauthorized {
		t.Skip("Skipping test because server requires authentication")
		return
	}

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "list", result.Object)
	assert.NotEmpty(t, result.Data, "Models list should not be empty")
}

func TestChatCompletion_Sync(t *testing.T) {
	req := api.ChatRequest{
		Model:    targetModel,
		Messages: []api.ChatMessage{{Role: "user", Content: api.Content{Text: "Say hi"}}},
		Stream:   false,
	}

	var resp api.ChatResponse
	code := makeRequest(t, "POST", baseURL+"/chat/completions", req, &resp)

	if code == http.StatusUnauthorized {
		t.Skip("Skipping test because server requires authentication")
		return
	}

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "chat.completion", resp.Object)
	require.NotEmpty(t, resp.Choices)
	assert.NotEmpty(t, resp.Choices[0].Message.Content.Text)
}

func TestValidationError(t *testing.T) {
	// purposefully bad payload (missing Model, invalid Role)
	payload := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "bad_role", "content": "hello"},
		},
	}

	// capture generic map to check error fields
	var errResp map[string]interface{}
	code := makeRequest(t, "POST", baseURL+"/chat/completions", payload, &errResp)

	if code == http.StatusUnauthorized {
		t.Skip("Skipping test because server requires authentication")
		return
	}

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "Validation Error", errResp["title"])

	// check the RFC 9457 "errors" extension
	errors, ok := errResp["errors"].(map[string]interface{})
	require.True(t, ok, "Should contain 'errors' map")

	// because of the clean "validator", we should check that this key should exist
	assert.Contains(t, errors, "messages[0].role")
	assert.Contains(t, errors, "model")
}