package huggingface

import (
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/openai"
)

const providerType = "huggingface"

func init() {
	llm.Register(providerType, NewAdapter)
}

func NewAdapter(cfg config.ProviderConfig) (llm.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://router.huggingface.co/v1"
	}
	return openai.NewAdapter(cfg)
}
