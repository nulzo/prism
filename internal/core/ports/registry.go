package ports

import "github.com/nulzo/model-router-api/pkg/schema"

type ModelRegistry interface {
	// GetModel returns the configuration for a given public model ID
	GetModel(id string) (*schema.ModelDefinition, error)
	
	// ListModels returns a list of all available models, optionally filtered
	ListModels() []schema.ModelDefinition
	
	// AddModel adds or updates a model definition dynamically
	AddModel(model schema.ModelDefinition)

	// ResolveRoute returns the provider ID and upstream model ID for a public model ID
	ResolveRoute(modelID string) (providerID string, upstreamModelID string, err error)
}
