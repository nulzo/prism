package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nulzo/model-router-api/internal/extension"
	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"github.com/nulzo/model-router-api/internal/plugin"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

// MaxAgenticIterations bounds the agentic tool-calling loop so a misbehaving
// model can't drive unbounded extension execution.
const MaxAgenticIterations = 15

// streamBufferSize sizes the outbound channel between the pipeline goroutine
// and the HTTP handler. Small enough to bound memory if the client is slow,
// large enough that bursty providers (multi-token batches) don't block.
const streamBufferSize = 16

// PrismToolEventAnnotationType is the discriminator we use to embed
// gateway-side tool execution events into a chat.completion.chunk's delta
// annotations. Standard OpenAI clients ignore unknown annotation types;
// UI clients (prism-ui) can subscribe to them to render tool activity.
const PrismToolEventAnnotationType = "prism.tool_event"

const finalAnswerAfterToolLimitPrompt = "You have reached the maximum number of tool calls. Do not call or request any more tools. Provide a final answer to the user based only on the information gathered so far."

func forceFinalAnswerAfterToolLimit(req *api.UpstreamChatRequest) {
	req.Messages = append(req.Messages, api.ChatMessage{
		Role: "system",
		Content: api.Content{
			Text: finalAnswerAfterToolLimitPrompt,
		},
	})
	req.Tools = nil
	req.ToolChoice = nil
}

// Stream runs a request through the plugin/extension pipeline as a streaming
// agentic loop:
//
//   - StreamChat exposes the channel directly to the HTTP handler.
//   - tests and internal callers can wrap it in Collect() to materialize a
//     single response.
//
// Lifecycle:
//
//  1. PreProcess plugins (synchronous; mutate ChatRequest in place).
//  2. Build UpstreamChatRequest, attach extension tool defs.
//  3. Loop up to MaxAgenticIterations:
//     a. Open upstream stream.
//     b. Forward content chunks to client; aggregate everything internally.
//     c. Hold trailing finish/usage chunks in a tail buffer until the
//     iteration ends so we can decide whether they belong to the client.
//     d. If any tool call hits a registered extension: drop the tail, execute
//     extensions, append messages, emit synthetic tool-event chunks, loop.
//     Else: flush the tail (final stop + usage) and exit.
//  4. PostProcess plugins on the aggregated response.
//
// Provider streams pass through unmodified at the chunk level so OpenAI
// SDKs see standard chat.completion.chunk objects. Extension execution is
// invisible to those SDKs except for the (ignored) annotation events.
func (o *PipelineOrchestrator) Stream(
	ctx context.Context,
	req *api.ChatRequest,
	provider llm.Provider,
) (<-chan api.StreamResult, error) {
	caps := llm.DescribeCapabilities(provider)
	log := logger.With(
		zap.String("provider", provider.Name()),
		zap.String("model", req.Model),
		zap.String("tool_calling_mode", string(caps.ToolCalling)),
	)

	pCtx := &plugin.Context{Request: req}
	activePlugins, err := o.runPreProcess(ctx, req, pCtx)
	if err != nil {
		return nil, err
	}

	upstreamReq := req.ToUpstream()
	upstreamReq.Stream = true

	activeExts, activeExtConfigs, err := o.bindExtensions(req, upstreamReq)
	if err != nil {
		return nil, err
	}
	if len(activeExts) > 0 && caps.ToolCalling == llm.ToolCallingUnsupported {
		return nil, api.BadRequestError("selected provider does not support extensions/tool calling")
	}

	out := make(chan api.StreamResult, streamBufferSize)
	go func() {
		defer close(out)
		final := o.runAgenticLoop(ctx, log, upstreamReq, provider, activeExts, activeExtConfigs, out)
		if final == nil {
			return // upstream errored or context cancelled; already drained
		}
		pCtx.Response = final.FinalResponse()
		for i := len(activePlugins) - 1; i >= 0; i-- {
			if err := activePlugins[i].PostProcess(ctx, pCtx); err != nil {
				log.Warn("plugin PostProcess failed",
					zap.String("plugin", activePlugins[i].ID()), zap.Error(err))
			}
		}
	}()
	return out, nil
}

// Collect drains a streaming pipeline channel into a single non-streaming
// response.
func Collect(ch <-chan api.StreamResult) (*api.ChatResponse, error) {
	agg := NewStreamAggregator()
	for r := range ch {
		if r.Err != nil {
			return nil, r.Err
		}
		agg.Apply(r.Response)
	}
	return agg.FinalResponse(), nil
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

func (o *PipelineOrchestrator) runPreProcess(
	ctx context.Context, req *api.ChatRequest, pCtx *plugin.Context,
) ([]plugin.Plugin, error) {
	active := make([]plugin.Plugin, 0, len(req.Plugins))
	for _, pConf := range req.Plugins {
		if pConf.Enabled != nil && !*pConf.Enabled {
			continue
		}
		p, err := o.plugins.Get(pConf.ID)
		if err != nil {
			continue // unknown plugin id is a soft error: log + ignore
		}
		if err := p.PreProcess(ctx, pCtx); err != nil {
			return nil, fmt.Errorf("plugin %s: %w", p.ID(), err)
		}
		active = append(active, p)
	}
	return active, nil
}

func (o *PipelineOrchestrator) bindExtensions(
	req *api.ChatRequest, upstreamReq *api.UpstreamChatRequest,
) (map[string]extension.Extension, map[string]api.ExtensionConfig, error) {
	active := make(map[string]extension.Extension)
	configs := make(map[string]api.ExtensionConfig)
	for _, ec := range req.Extensions {
		if ec.Enabled != nil && !*ec.Enabled {
			continue
		}
		ext, err := o.extensions.Get(ec.ID)
		if err != nil {
			continue
		}
		toolDef, err := ext.BuildTool(ec)
		if err != nil {
			return nil, nil, err
		}
		upstreamReq.Tools = append(upstreamReq.Tools, toolDef)
		// Key by the wire function name so lookups on incoming tool_calls
		// (which carry the sanitised name the LLM saw) resolve directly
		// without a second indirection through ext.ID().
		active[toolDef.Function.Name] = ext
		configs[toolDef.Function.Name] = ec
	}
	return active, configs, nil
}

// runAgenticLoop is the heart of streaming-with-tools. It returns the final
// aggregator (so the caller can run PostProcess plugins on the assembled
// response) or nil if the stream was cancelled / errored.
func (o *PipelineOrchestrator) runAgenticLoop(
	ctx context.Context,
	log *zap.Logger,
	upstreamReq *api.UpstreamChatRequest,
	provider llm.Provider,
	activeExts map[string]extension.Extension,
	activeExtConfigs map[string]api.ExtensionConfig,
	out chan<- api.StreamResult,
) *StreamAggregator {
	hasExtensions := len(activeExts) > 0

	for iter := 0; iter <= MaxAgenticIterations; iter++ {
		log.Debug("provider stream iteration", zap.Int("iteration", iter+1))

		upstream, err := provider.Stream(ctx, upstreamReq)
		if err != nil {
			sendOrCancel(ctx, out, api.StreamResult{Err: err})
			return nil
		}

		agg := NewStreamAggregator()
		tail := newTailBuffer(4)

		for result := range upstream {
			if result.Err != nil {
				sendOrCancel(ctx, out, result)
				return nil
			}
			agg.Apply(result.Response)
			if !hasExtensions || !isTailLike(result.Response) {
				if !tail.Flush(ctx, out) {
					return nil
				}
				if !sendOrCancel(ctx, out, result) {
					return nil
				}
				continue
			}
			if !tail.Push(ctx, out, result) {
				return nil
			}
		}

		// Decide if this iteration triggers another loop. Presence of a
		// matching tool call is the sole trigger — `finish_reason` is a
		// hint at best. Several OpenAI-compat providers (Google Gemini,
		// Moonshot, some Qwen builds) emit tool calls under
		// `finish_reason: "stop"` or with no finish_reason at all, so
		// gating on finish_reason silently drops tool execution on those
		// providers. If the model produced a call that maps to one of
		// our registered extensions, we execute — the model asked us to.
		log.Debug("iteration summary",
			zap.String("finish_reason", agg.FinishReason()),
			zap.Int("tool_calls", len(agg.ToolCalls())),
			zap.Int("content_bytes", len(agg.AssistantMessage().Content.Text)),
		)
		if hasExtensions && iter < MaxAgenticIterations && hasMatchingExtension(agg.ToolCalls(), activeExts) {
			tail.Drop() // suppress trailing tool_calls/stop + usage-only chunks
			emitToolEvents(ctx, out, agg.ToolCalls(), activeExts, "tool_call", nil)
			if !o.executeExtensions(ctx, log, agg, activeExts, activeExtConfigs, upstreamReq, out) {
				return nil
			}

			// If this was the last allowed tool-calling iteration, ask for one
			// final answer with tool-calling removed from the provider request.
			if iter == MaxAgenticIterations-1 {
				log.Warn("agentic loop hit MaxAgenticIterations, forcing final answer", zap.Int("max", MaxAgenticIterations))
				forceFinalAnswerAfterToolLimit(upstreamReq)
			}
			continue
		}

		// Final iteration: flush the buffered tail (stop + usage) to the client.
		if !tail.Flush(ctx, out) {
			return nil
		}
		return agg
	}

	// Unreachable: the loop always returns either an aggregator or nil.
	return nil
}

// executeExtensions runs each tool call that maps to an active extension,
// appending the assistant turn (with tool_calls) and the tool-result turn to
// the upstream message history so the next loop iteration sees them.
//
// Tool calls that don't map to any extension are left in the assistant
// message but generate no tool-result turn — the next iteration will see
// the same call and likely just emit a final answer.
func (o *PipelineOrchestrator) executeExtensions(
	ctx context.Context,
	log *zap.Logger,
	agg *StreamAggregator,
	activeExts map[string]extension.Extension,
	activeExtConfigs map[string]api.ExtensionConfig,
	upstreamReq *api.UpstreamChatRequest,
	out chan<- api.StreamResult,
) bool {
	upstreamReq.Messages = append(upstreamReq.Messages, agg.AssistantMessage())

	for _, tc := range agg.ToolCalls() {
		ext, ok := activeExts[tc.Function.Name]
		if !ok {
			continue
		}

		rawArgs := tc.Function.Arguments

		log.Debug("executing extension",
			zap.String("extension", tc.Function.Name),
			zap.String("tool_call_id", tc.ID),
			zap.Int("arguments_bytes", len(rawArgs)),
		)
		result, err := ext.Execute(ctx, activeExtConfigs[tc.Function.Name], []byte(rawArgs))
		if err != nil {
			log.Warn("extension execution failed",
				zap.String("extension", tc.Function.Name),
				zap.String("tool_call_id", tc.ID),
				zap.Error(err),
			)
			log.Debug("extension execution failed: raw arguments",
				zap.String("extension", tc.Function.Name),
				zap.String("raw_arguments", rawArgs),
			)
			result = fmt.Sprintf(`{"error":%q}`, err.Error())
		}

		emitToolEvents(ctx, out, []api.ToolCall{tc}, activeExts, "tool_result", map[string]string{tc.ID: result})

		upstreamReq.Messages = append(upstreamReq.Messages, api.ChatMessage{
			Role:       "tool",
			ToolCallID: tc.ID,
			Name:       tc.Function.Name,
			Content:    api.Content{Text: result},
		})

		if ctx.Err() != nil {
			return false
		}
	}
	return true
}

// hasMatchingExtension reports whether any tool call in the slice belongs to
// the gateway's registered extensions.
func hasMatchingExtension(calls []api.ToolCall, exts map[string]extension.Extension) bool {
	for _, tc := range calls {
		if _, ok := exts[tc.Function.Name]; ok {
			return true
		}
	}
	return false
}

// isTailLike returns true if a chunk is a likely end-of-iteration marker
// (finish chunk or usage-only). We hold these in a tail buffer so that if
// the iteration loops we can drop them silently.
func isTailLike(r *api.ChatResponse) bool {
	if r == nil {
		return false
	}
	if len(r.Choices) == 0 {
		// Pure usage chunk (OpenAI emits this after finish_reason when
		// stream_options.include_usage is true).
		return r.Usage != nil
	}
	for _, ch := range r.Choices {
		if ch.FinishReason != "" {
			return true
		}
	}
	return false
}

// sendOrCancel forwards a chunk to the client unless the context is done.
// Returns false if the receiver disconnected so callers can short-circuit.
func sendOrCancel(ctx context.Context, out chan<- api.StreamResult, r api.StreamResult) bool {
	select {
	case out <- r:
		return true
	case <-ctx.Done():
		return false
	}
}

// emitToolEvents emits one synthetic chat.completion.chunk per tool call,
// embedding gateway-side execution metadata in the delta annotations. UI
// clients that subscribe to `event: prism.tool_event` (see chat_handler) can
// use these to render activity indicators; standard SDKs treat them as
// no-op assistant deltas with empty content.
func emitToolEvents(
	ctx context.Context,
	out chan<- api.StreamResult,
	calls []api.ToolCall,
	activeExts map[string]extension.Extension,
	kind string,
	results map[string]string,
) {
	for _, tc := range calls {
		if _, ok := activeExts[tc.Function.Name]; !ok {
			continue
		}
		payload := map[string]interface{}{
			"kind":      kind,
			"tool_name": tc.Function.Name,
			"tool_id":   tc.ID,
			"arguments": tc.Function.Arguments,
		}
		if results != nil {
			if r, ok := results[tc.ID]; ok {
				payload["result"] = r
			}
		}
		raw, _ := json.Marshal(payload)
		chunk := &api.ChatResponse{
			Object: "chat.completion.chunk",
			Choices: []api.Choice{{
				Index: 0,
				Delta: &api.ChatMessage{
					Role: "assistant",
					Annotations: []interface{}{map[string]interface{}{
						"type":    PrismToolEventAnnotationType,
						"payload": json.RawMessage(raw),
					}},
				},
			}},
		}
		sendOrCancel(ctx, out, api.StreamResult{Response: chunk})
	}
}

// ---------------------------------------------------------------------------
// tail buffer
// ---------------------------------------------------------------------------

// tailBuffer holds the latest N "tail-like" chunks from the upstream stream
// so that the pipeline can decide post-hoc whether to forward them to the
// client (final iteration) or drop them (intermediate tool-call iteration).
type tailBuffer struct {
	cap   int
	items []api.StreamResult
}

func newTailBuffer(cap int) *tailBuffer { return &tailBuffer{cap: cap} }

// Push adds a result to the buffer. If the buffer is full the oldest item
// is flushed to the client first to bound memory and avoid swallowing real
// content if a misbehaving provider sends >cap consecutive tail-like chunks.
func (b *tailBuffer) Push(ctx context.Context, out chan<- api.StreamResult, r api.StreamResult) bool {
	if len(b.items) >= b.cap {
		head := b.items[0]
		b.items = b.items[1:]
		if !sendOrCancel(ctx, out, head) {
			return false
		}
	}
	b.items = append(b.items, r)
	return true
}

// Flush emits all buffered items in order and clears the buffer. Used when
// the iteration ended with a non-tool finish reason.
func (b *tailBuffer) Flush(ctx context.Context, out chan<- api.StreamResult) bool {
	for _, r := range b.items {
		if !sendOrCancel(ctx, out, r) {
			return false
		}
	}
	b.items = nil
	return true
}

// Drop discards all buffered items without forwarding. Used when the
// iteration ended with a tool_calls finish that the gateway will execute,
// so the client must not see the empty finish/usage chunks.
func (b *tailBuffer) Drop() { b.items = nil }
