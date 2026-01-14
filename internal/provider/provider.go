package provider

import (
	"context"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type ProviderConfig struct {
	APIKey  string
	BaseURL string
	Version string // For providers like Anthropic that require version headers
}

type StreamResult struct {
	Response *schema.ChatResponse
	Err      error
}

type ModelProvider interface {
	Name() string
	// Chat handles non-streaming requests
	Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error)
	// Stream handles streaming requests
	Stream(ctx context.Context, req *schema.ChatRequest) (<-chan StreamResult, error)
}
