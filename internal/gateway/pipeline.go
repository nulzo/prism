package gateway

import (
	"context"

	"github.com/nulzo/model-router-api/internal/extension"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/plugin"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

// PipelineOrchestrator handles the execution of plugins and extensions around the LLM call.
type PipelineOrchestrator struct {
	plugins    *plugin.Registry
	extensions *extension.Registry
}

func NewPipelineOrchestrator(p *plugin.Registry, e *extension.Registry) *PipelineOrchestrator {
	return &PipelineOrchestrator{
		plugins:    p,
		extensions: e,
	}
}

// Execute runs the full request pipeline: PreProcess -> Agentic Loop -> PostProcess.
// Router-only concerns (plugins/extensions/config) stay on ChatRequest while the
// provider only receives a sanitized UpstreamChatRequest.
func (o *PipelineOrchestrator) Execute(ctx context.Context, req *api.ChatRequest, provider llm.Provider) (*api.ChatResponse, error) {
	pCtx := &plugin.Context{Request: req}
	log := logger.With(
		zap.String("provider", provider.Name()),
		zap.String("model", req.Model),
		zap.String("tool_calling_mode", string(llm.DescribeCapabilities(provider).ToolCalling)),
	)

	// 1. PreProcess Plugins
	activePlugins := make([]plugin.Plugin, 0)
	for _, pConf := range req.Plugins {
		if pConf.Enabled != nil && !*pConf.Enabled {
			continue
		}
		if p, err := o.plugins.Get(pConf.ID); err == nil {
			activePlugins = append(activePlugins, p)
			log.Debug("enabling plugin", zap.String("plugin", p.ID()))
			if err := p.PreProcess(ctx, pCtx); err != nil {
				return nil, err
			}
		} else {
			log.Warn("requested plugin not found", zap.String("plugin", pConf.ID))
		}
	}

	upstreamReq := req.ToUpstream()

	// 2. Inject requested Extensions into the provider-safe request
	activeExtensions := make(map[string]extension.Extension)
	activeExtensionConfigs := make(map[string]api.ExtensionConfig)
	for _, extConf := range req.Extensions {
		// Skip if explicitly disabled
		if extConf.Enabled != nil && !*extConf.Enabled {
			continue
		}

		// Look up the extension in the registry
		if ext, err := o.extensions.Get(extConf.ID); err == nil {
			toolDef, buildErr := ext.BuildTool(extConf)
			if buildErr != nil {
				return nil, buildErr
			}
			upstreamReq.Tools = append(upstreamReq.Tools, toolDef)
			// Key by wire function name so tool_call lookups match the
			// sanitised identifier the LLM was shown.
			activeExtensions[toolDef.Function.Name] = ext
			activeExtensionConfigs[toolDef.Function.Name] = extConf
			log.Debug("enabling extension",
				zap.String("extension_id", ext.ID()),
				zap.String("tool_name", toolDef.Function.Name),
			)
		} else {
			log.Warn("requested extension not found", zap.String("extension", extConf.ID))
		}
	}

	log.Debug("prepared request pipeline",
		zap.Int("plugins_enabled", len(activePlugins)),
		zap.Int("extensions_enabled", len(activeExtensions)),
		zap.Int("tools_sent_upstream", len(upstreamReq.Tools)),
	)

	capabilities := llm.DescribeCapabilities(provider)
	if len(activeExtensions) > 0 && capabilities.ToolCalling == llm.ToolCallingUnsupported {
		return nil, api.BadRequestError("selected provider does not support extensions/tool calling")
	}

	// 3. Agentic Loop
	var finalResp *api.ChatResponse
	maxIterations := 5

	for i := 0; i < maxIterations; i++ {
		log.Debug("provider chat iteration", zap.Int("iteration", i+1))
		resp, err := provider.Chat(ctx, upstreamReq)
		if err != nil {
			return nil, err
		}

		finalResp = resp
		if len(resp.Choices) > 0 {
			toolCalls := 0
			if resp.Choices[0].Message != nil {
				toolCalls = len(resp.Choices[0].Message.ToolCalls)
			}
			log.Debug("provider returned choice",
				zap.Int("iteration", i+1),
				zap.String("finish_reason", resp.Choices[0].FinishReason),
				zap.Int("tool_calls", toolCalls),
			)
		}

		// Presence of a matching tool call — not `finish_reason` —
		// drives the loop. Several OpenAI-compat providers (Gemini,
		// Moonshot) emit tool calls under `finish_reason: "stop"`, so
		// gating on finish_reason silently swallows tool execution.
		if len(resp.Choices) > 0 && resp.Choices[0].Message != nil && len(resp.Choices[0].Message.ToolCalls) > 0 {
			hasExtension := false

			// Append the assistant's tool call message to history
			upstreamReq.Messages = append(upstreamReq.Messages, *resp.Choices[0].Message)

			for _, tc := range resp.Choices[0].Message.ToolCalls {
				// Is it an active extension?
				if ext, ok := activeExtensions[tc.Function.Name]; ok {
					hasExtension = true
					// Defense-in-depth: same scanner-based normalization
					// the streaming pipeline uses. Non-streaming tool
					// calls go through adapter.Chat so the raw
					// accumulator isn't involved, but providers still
					// occasionally return slightly malformed arguments
					// (extra whitespace, trailing commas on legacy OSS
					// models) and this guarantees we hand Execute a
					// single valid JSON value.
					rawArgs := tc.Function.Arguments
					args := SanitizeArguments(rawArgs)
					log.Debug("executing extension",
						zap.String("extension", tc.Function.Name),
						zap.String("tool_call_id", tc.ID),
						zap.Int("arguments_bytes", len(args)),
					)

					resultStr, execErr := ext.Execute(ctx, activeExtensionConfigs[tc.Function.Name], []byte(args))
					if execErr != nil {
						log.Warn("extension execution failed",
							zap.String("extension", tc.Function.Name),
							zap.String("tool_call_id", tc.ID),
							zap.Error(execErr),
						)
						log.Debug("extension execution failed: raw arguments",
							zap.String("extension", tc.Function.Name),
							zap.String("raw_arguments", rawArgs),
							zap.String("sanitized_arguments", args),
						)
						resultStr = `{"error": "` + execErr.Error() + `"}`
					}

					// Append the extension result to the messages
					upstreamReq.Messages = append(upstreamReq.Messages, api.ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    api.Content{Text: resultStr},
					})
				} else {
					log.Debug("ignoring non-extension tool call", zap.String("tool", tc.Function.Name))
				}
			}

			// If we executed at least one extension, loop again
			if hasExtension {
				continue
			}
		}

		// If no extensions were called, break the loop
		break
	}

	pCtx.Response = finalResp

	// 4. PostProcess Plugins (in reverse order)
	for i := len(activePlugins) - 1; i >= 0; i-- {
		if err := activePlugins[i].PostProcess(ctx, pCtx); err != nil {
			return nil, err
		}
	}

	return finalResp, nil
}
