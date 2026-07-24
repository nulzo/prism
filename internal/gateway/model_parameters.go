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

type chatParameterPolicy struct {
	names []string
	clear func(*api.ChatRequest)
}

var chatParameterPolicies = []chatParameterPolicy{
	{
		names: []string{"temperature"},
		clear: func(req *api.ChatRequest) { req.Temperature = 0 },
	},
	{
		names: []string{"top_p"},
		clear: func(req *api.ChatRequest) { req.TopP = 0 },
	},
	{
		names: []string{"top_k"},
		clear: func(req *api.ChatRequest) { req.TopK = 0 },
	},
	{
		names: []string{"max_tokens", "max_completion_tokens"},
		clear: func(req *api.ChatRequest) {
			req.MaxTokens = 0
			req.MaxCompletionTokens = 0
		},
	},
	{
		names: []string{"stop"},
		clear: func(req *api.ChatRequest) { req.Stop = nil },
	},
	{
		names: []string{"response_format", "structured_outputs"},
		clear: func(req *api.ChatRequest) { req.ResponseFormat = nil },
	},
	{
		names: []string{"tools"},
		clear: func(req *api.ChatRequest) { req.Tools = nil },
	},
	{
		names: []string{"tool_choice"},
		clear: func(req *api.ChatRequest) { req.ToolChoice = nil },
	},
	{
		names: []string{"frequency_penalty"},
		clear: func(req *api.ChatRequest) { req.FrequencyPenalty = 0 },
	},
	{
		names: []string{"presence_penalty"},
		clear: func(req *api.ChatRequest) { req.PresencePenalty = 0 },
	},
	{
		names: []string{"repetition_penalty"},
		clear: func(req *api.ChatRequest) { req.RepetitionPenalty = 0 },
	},
	{
		names: []string{"seed"},
		clear: func(req *api.ChatRequest) { req.Seed = 0 },
	},
	{
		names: []string{"logit_bias"},
		clear: func(req *api.ChatRequest) { req.LogitBias = nil },
	},
	{
		names: []string{"top_logprobs", "logprobs"},
		clear: func(req *api.ChatRequest) { req.TopLogprobs = 0 },
	},
	{
		names: []string{"min_p"},
		clear: func(req *api.ChatRequest) { req.MinP = 0 },
	},
	{
		names: []string{"top_a"},
		clear: func(req *api.ChatRequest) { req.TopA = 0 },
	},
	{
		names: []string{"prediction"},
		clear: func(req *api.ChatRequest) { req.Prediction = nil },
	},
	{
		names: []string{"modalities"},
		clear: func(req *api.ChatRequest) { req.Modalities = nil },
	},
	{
		names: []string{"audio"},
		clear: func(req *api.ChatRequest) { req.Audio = nil },
	},
}

// applyModelParameterPolicy removes normalized controls that the catalog says
// the target model does not support. This keeps Prism's public API tolerant:
// callers may send OpenRouter-style sampling and reasoning preferences
// globally, while provider requests only include native knobs for compatible
// models.
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

	supported := supportedParameterSet(model.SupportedParameters)

	if !modelSupportsAnyParameter(supported, reasoningParameterAliases...) {
		req.Reasoning = nil
		req.IncludeReasoning = nil
	}

	for _, policy := range chatParameterPolicies {
		if modelSupportsAnyParameter(supported, policy.names...) {
			continue
		}
		policy.clear(req)
	}
}

func supportedParameterSet(parameters []string) map[string]struct{} {
	supported := make(map[string]struct{}, len(parameters))
	for _, name := range parameters {
		supported[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	return supported
}

func modelSupportsAnyParameter(supported map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := supported[strings.ToLower(strings.TrimSpace(name))]; ok {
			return true
		}
	}
	return false
}
