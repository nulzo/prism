package ollama

import (
	"github.com/nulzo/model-router-api/internal/adapters/providers/openai"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/registry"
)

func init() {
	registry.Register("ollama", NewAdapter)
}

// NewAdapter creates an Ollama adapter. 
// Since Ollama is OpenAI-compatible at /v1, we can reuse the OpenAI adapter logic!
// This is the ultimate DRY principle.
func NewAdapter(config domain.ProviderConfig) (ports.ModelProvider, error) {
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434/v1" // Ollama's OpenAI compatible endpoint
	}
	
	// Create the OpenAI adapter but acting as Ollama
	return openai.NewAdapter(config)
}
