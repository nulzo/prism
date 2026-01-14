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
	
	// Future proofing
	// ImageGen(ctx context.Context, req *schema.ImageRequest) (...)
}

type StreamResult struct {
	Response *schema.ChatResponse
	Err      error
}

// RouterService defines the business logic for routing requests.
type RouterService interface {
	GetProviderForModel(ctx context.Context, modelID string) (ModelProvider, error)
	ListAllModels(ctx context.Context) ([]schema.Model, error)
	Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error)
	StreamChat(ctx context.Context, req *schema.ChatRequest) (<-chan StreamResult, error)
}
