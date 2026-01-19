package llm

import (
	"context"

	"github.com/nulzo/model-router-api/pkg/api"
)

type ProviderName string

const (
	Ollama    ProviderName = "ollama"
	OpenAI    ProviderName = "openai"
	Anthropic ProviderName = "anthropic"
	Google    ProviderName = "google"
)

type Provider interface {
	Name() string
	Type() string // e.g., "openai", "anthropic"
	Chat(ctx context.Context, req *api.ChatRequest) (*api.ChatResponse, error)
	Stream(ctx context.Context, req *api.ChatRequest) (<-chan api.StreamResult, error)
	Models(ctx context.Context) ([]api.ModelDefinition, error)
}
