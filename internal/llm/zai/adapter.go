package zai

import (
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/llm/openai"
)

const providerType = "zai"

func init() {
	llm.Register(providerType, NewAdapter)
}

func NewAdapter(cfg config.ProviderConfig) (llm.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.z.ai/api/paas/v4"
	}
	return openai.NewAdapter(cfg)
}
