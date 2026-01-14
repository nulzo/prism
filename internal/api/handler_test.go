package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/api"
	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/internal/router"
	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockProvider is a mock implementation of provider.ModelProvider
type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockProvider) Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.ChatResponse), args.Error(1)
}

func (m *MockProvider) Stream(ctx context.Context, req *schema.ChatRequest) (<-chan provider.StreamResult, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(<-chan provider.StreamResult), args.Error(1)
}

func setupRouter(p provider.ModelProvider) *gin.Engine {
	r := router.NewRouter()
	r.RegisterProvider(p)
	r.RegisterRoute("test-model", p.Name())

	h := api.NewHandler(r)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h.RegisterRoutes(engine)
	return engine
}

func TestHandleChatCompletions_Success(t *testing.T) {
	mockP := new(MockProvider)
	mockP.On("Name").Return("mock-provider")
	
	expectedResp := &schema.ChatResponse{
		ID:    "test-id",
		Model: "test-model",
		Choices: []schema.Choice{
			{Message: &schema.ChatMessage{Role: "assistant", Content: schema.Content{Text: "Hello"}}},
		},
	}
	
	mockP.On("Chat", mock.Anything, mock.MatchedBy(func(req *schema.ChatRequest) bool {
		return req.Model == "test-model"
	})).Return(expectedResp, nil)

	engine := setupRouter(mockP)

	reqBody := schema.ChatRequest{
		Model:    "test-model",
		Messages: []schema.ChatMessage{{Role: "user", Content: schema.Content{Text: "Hi"}}},
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	
	var actualResp schema.ChatResponse
	err := json.Unmarshal(w.Body.Bytes(), &actualResp)
	assert.NoError(t, err)
	assert.Equal(t, "test-id", actualResp.ID)
	assert.Equal(t, "Hello", actualResp.Choices[0].Message.Content.Text)
}

func TestHandleChatCompletions_Stream(t *testing.T) {
	mockP := new(MockProvider)
	mockP.On("Name").Return("mock-provider")

	// Create a channel for streaming
	ch := make(chan provider.StreamResult, 2)
	ch <- provider.StreamResult{
		Response: &schema.ChatResponse{
			ID: "stream-id",
			Choices: []schema.Choice{{Delta: &schema.ChatMessage{Content: schema.Content{Text: "Hel"}}}},
		},
	}
	ch <- provider.StreamResult{
		Response: &schema.ChatResponse{
			ID: "stream-id",
			Choices: []schema.Choice{{Delta: &schema.ChatMessage{Content: schema.Content{Text: "lo"}}}},
		},
	}
	close(ch)

	mockP.On("Stream", mock.Anything, mock.MatchedBy(func(req *schema.ChatRequest) bool {
		return req.Stream
	})).Return((<-chan provider.StreamResult)(ch), nil)

	engine := setupRouter(mockP)

	reqBody := schema.ChatRequest{
		Model:    "test-model",
		Messages: []schema.ChatMessage{{Role: "user", Content: schema.Content{Text: "Hi"}}},
		Stream:   true,
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
	
	bodyStr := w.Body.String()
	assert.Contains(t, bodyStr, "data: {")
	assert.Contains(t, bodyStr, "Hel")
	assert.Contains(t, bodyStr, "lo")
	assert.Contains(t, bodyStr, "[DONE]")
}

func TestHandleChatCompletions_ProviderError(t *testing.T) {
	mockP := new(MockProvider)
	mockP.On("Name").Return("mock-provider")

	mockP.On("Chat", mock.Anything, mock.Anything).Return((*schema.ChatResponse)(nil), errors.New("provider failed"))

	engine := setupRouter(mockP)

	reqBody := schema.ChatRequest{
		Model:    "test-model",
		Messages: []schema.ChatMessage{{Role: "user", Content: schema.Content{Text: "Hi"}}},
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleChatCompletions_InvalidBody(t *testing.T) {
	mockP := new(MockProvider)
	mockP.On("Name").Return("mock-provider")
	engine := setupRouter(mockP)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString("{invalid-json"))
	req.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
