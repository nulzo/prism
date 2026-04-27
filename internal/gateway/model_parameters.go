package gateway

import (
	"strings"

	"github.com/nulzo/model-router-api/pkg/api"
)

var reasoningParameterAliases = []string{
	"reasoning",
	"include_reasoning",
	"reasoning_effort",
}

// applyModelParameterPolicy removes normalized controls that the catalog says
// the target model does not support. This keeps Prism's public API tolerant:
// callers may send OpenRouter-style reasoning preferences globally, while
// provider requests only include native reasoning knobs for compatible models.
func (s *service) applyModelParameterPolicy(req *api.ChatRequest, publicModelID string) {
	if req == nil {
		return
	}

	model, ok := s.modelDefinition(publicModelID)
	if !ok {
		// Pass-through models do not have catalog metadata; leave the request
		// untouched and let the provider adapter handle its native contract.
		return
	}

	sanitizeRequestForModel(req, model)
}

func (s *service) modelDefinition(modelID string) (api.ModelDefinition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	model, ok := s.models[modelID]
	return model, ok
}

func sanitizeRequestForModel(req *api.ChatRequest, model api.ModelDefinition) {
	if req == nil {
		return
	}
	if modelSupportsAnyParameter(model, reasoningParameterAliases...) {
		return
	}

	req.Reasoning = nil
	req.IncludeReasoning = nil
}

func modelSupportsAnyParameter(model api.ModelDefinition, names ...string) bool {
	for _, supported := range model.SupportedParameters {
		for _, name := range names {
			if strings.EqualFold(supported, name) {
				return true
			}
		}
	}
	return false
}
