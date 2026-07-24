package ollama

import (
	"testing"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter_Capabilities(t *testing.T) {
	adapter, err := NewAdapter(config.ProviderConfig{
		ID:      "ollama",
		Type:    "ollama",
		BaseURL: "http://127.0.0.1:11434",
	})
	require.NoError(t, err)

	describer, ok := adapter.(llm.CapabilityDescriber)
	require.True(t, ok)
	assert.Equal(t, llm.ToolCallingOpenAICompat, describer.Capabilities().ToolCalling)
}
