package harvester

import (
	"testing"
	"time"

	"github.com/nulzo/model-router-api/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestMergeModels(t *testing.T) {
	h := &HarvesterService{}

	// Scenario:
	// 1. "gpt-4" exists in YAML (Manual)
	// 2. "gpt-4" exists in Live (should verify it exists, maybe update timestamp)
	// 3. "gpt-5" exists in Live but NOT in YAML (should be added as disabled)
	
	existing := []schema.ModelDefinition{
		{
			ID:          "openai/gpt-4",
			UpstreamID:  "gpt-4",
			Name:        "GPT 4 Manual",
			Pricing:     schema.ModelPricing{Input: 1.0},
			Source:      "manual",
			LastUpdated: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	live := []schema.Model{
		{ID: "gpt-4"}, // Matches existing
		{ID: "gpt-5"}, // New
	}

	result := h.mergeModels(existing, live, "openai-main")

	assert.Equal(t, 2, len(result))

	// Verify Existing Model Preserved
	m1 := result[0]
	assert.Equal(t, "openai/gpt-4", m1.ID)
	assert.Equal(t, "GPT 4 Manual", m1.Name) // Name should NOT change
	assert.Equal(t, 1.0, m1.Pricing.Input)   // Pricing should NOT change
	assert.True(t, m1.LastUpdated.After(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)))

	// Verify New Model Added
	m2 := result[1]
	assert.Equal(t, "openai-main/gpt-5", m2.ID) // Generated ID
	assert.Equal(t, "gpt-5", m2.UpstreamID)
	assert.False(t, m2.Enabled) // Should be disabled
	assert.Equal(t, "auto", m2.Source)
}
