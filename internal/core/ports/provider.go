package ports

import (
	"context"
	"github.com/nulzo/model-router-api/pkg/schema"
)

// ModelProvider defines the contract that all AI providers must implement.
type ModelProvider interface {
	Name() string
	Type() string // e.g., "openai", "anthropic"
	
	// Core capabilities
	Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error)
	Stream(ctx context.Context, req *schema.ChatRequest) (<-chan StreamResult, error)
	Models(ctx context.Context) ([]schema.Model, error)
}

type StreamResult struct {
	Response *schema.ChatResponse
	Err      error
}
