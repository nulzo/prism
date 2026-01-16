package ports

import (
	"context"

	"github.com/nulzo/model-router-api/pkg/schema"
)

type ModelFilter struct {
	Provider string
	ID       string
	Modality string
	OwnedBy  string
}

// RouterService defines the business logic for routing requests.
type RouterService interface {
	GetProviderForModel(ctx context.Context, modelID string) (ModelProvider, string, error)
	ListAllModels(ctx context.Context, filter ModelFilter) ([]schema.Model, error)
	Chat(ctx context.Context, req *schema.ChatRequest) (*schema.ChatResponse, error)
	StreamChat(ctx context.Context, req *schema.ChatRequest) (<-chan StreamResult, error)
}
