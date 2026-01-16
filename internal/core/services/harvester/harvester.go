package harvester

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type HarvesterService struct {
	logger        *zap.Logger
	registry      ports.ModelRegistry
	providers     map[string]ports.ModelProvider
	configDirPath string // e.g., "internal/config/models"
}

func NewHarvesterService(logger *zap.Logger, registry ports.ModelRegistry, providers map[string]ports.ModelProvider, configPath string) *HarvesterService {
	return &HarvesterService{
		logger:        logger,
		registry:      registry,
		providers:     providers,
		configDirPath: configPath,
	}
}

// HarvestProvider syncs a single provider's live models with its YAML config
func (h *HarvesterService) HarvestProvider(ctx context.Context, providerID string, providerType string) error {
	provider, ok := h.providers[providerID]
	if !ok {
		return fmt.Errorf("provider %s not found in active providers", providerID)
	}

	// 1. Fetch Live Models
	liveModels, err := provider.Models(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch live models from %s: %w", providerID, err)
	}

	// 2. Load Existing YAML
	filePath := fmt.Sprintf("%s/%s.yaml", h.configDirPath, providerType)
	existingModels, err := h.loadYamlModels(filePath)
	if err != nil {
		// If file doesn't exist, we start with empty list
		h.logger.Warn("Could not load existing config, creating new", zap.String("file", filePath))
		existingModels = []schema.ModelDefinition{}
	}

	// 3. Merge Logic
	updatedModels := h.mergeModels(existingModels, liveModels, providerID)

	// 4. Write Back to YAML
	if err := h.saveYamlModels(filePath, updatedModels); err != nil {
		return fmt.Errorf("failed to save config to %s: %w", filePath, err)
	}
	
	// 5. Update In-Memory Registry
	for _, m := range updatedModels {
		h.registry.AddModel(m)
	}

	h.logger.Info("Harvest complete", zap.String("provider", providerID), zap.Int("models_count", len(updatedModels)))
	return nil
}

func (h *HarvesterService) mergeModels(existing []schema.ModelDefinition, live []schema.Model, providerID string) []schema.ModelDefinition {
	modelMap := make(map[string]int)
	for i, m := range existing {
		// Use UpstreamID as the unique key for matching, because ID (public) is arbitrary
		key := m.UpstreamID
		// Fallback for older configs
		if key == "" { key = m.ID }
		modelMap[key] = i
	}

	for _, liveModel := range live {
		// Normalize ID: liveModel.ID from provider is usually the upstream ID
		upstreamID := liveModel.ID
		// For some providers (like Ollama), the ID is what we use. For OpenAI, it's just the model ID.
		
		idx, exists := modelMap[upstreamID]
		
		if exists {
			// UPDATE existing model
			// We only update fields that are safe to auto-update
			existing[idx].LastUpdated = time.Now()
			// Note: We do NOT overwrite Name, Description, Pricing, or Config as those might be manually tuned
		} else {
			// CREATE new model
			newModel := schema.ModelDefinition{
				ID:          fmt.Sprintf("%s/%s", providerID, upstreamID), // Generate a default Public ID
				Name:        liveModel.ID, // Default name
				ProviderID:  providerID,
				UpstreamID:  upstreamID,
				Description: fmt.Sprintf("Imported from %s", providerID),
				Enabled:     false, // Disabled by default for safety
				Source:      "auto",
				LastUpdated: time.Now(),
				Pricing: schema.ModelPricing{
					Input: 0, Output: 0, // Needs manual intervention
				},
				Config: schema.ModelConfig{
					Modality: []string{"text"}, // Default
				},
			}
			existing = append(existing, newModel)
		}
	}
	return existing
}

func (h *HarvesterService) loadYamlModels(path string) ([]schema.ModelDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Models []schema.ModelDefinition `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Models, nil
}

func (h *HarvesterService) saveYamlModels(path string, models []schema.ModelDefinition) error {
	wrapper := struct {
		Models []schema.ModelDefinition `yaml:"models"`
	}{
		Models: models,
	}
	data, err := yaml.Marshal(wrapper)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
