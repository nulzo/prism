package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_Defaults(t *testing.T) {

	os.Clearenv()
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("SERVER_ENV", "test")
	t.Setenv("REDIS_ENABLED", "true")

	cfg, err := LoadConfig()
	assert.NoError(t, err)

	assert.Equal(t, "9090", cfg.Server.Port)
	assert.Equal(t, "test", cfg.Server.Env)
	assert.True(t, cfg.Redis.Enabled)
}

func TestLoadConfig_APIKeyResolution(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-12345")

	configContent := `
providers:
  - id: "test-provider"
    name: "Test"
    type: "test"
    api_key: "ENV:TEST_API_KEY"
    enabled: true
`
	f, err := os.CreateTemp("", "config_*.yaml")
	assert.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = f.WriteString(configContent)
	assert.NoError(t, err)
	f.Close()
}
