package gateway

import (
	"encoding/json"
	"strings"

	"github.com/nulzo/model-router-api/pkg/api"
)

// ToolCallAccumulator merges OpenAI-style streaming `tool_call` deltas into
// complete tool calls. Each delta references its slot by `index`; subsequent
// deltas append to `function.arguments` while `id` and `function.name` are
// usually only present on the first delta for that index.
//
// Provider-agnostic: anything that emits tool_calls in the OpenAI delta shape
// (OpenAI, OpenAI-compat Google/Qwen/Moonshot, and our own injected synthetic
// chunks) merges into the same accumulator. Non-OpenAI providers that need
// to map upstream-specific delta shapes can construct their slot index
// explicitly via ApplyAt.
type ToolCallAccumulator struct {
	bySlot map[int]*api.ToolCall
	maxIdx int
}

func NewToolCallAccumulator() *ToolCallAccumulator {
	return &ToolCallAccumulator{bySlot: map[int]*api.ToolCall{}, maxIdx: -1}
}

// Apply merges a single delta chunk's tool_calls into the accumulator using
// positional order as the slot key. This matches OpenAI/Google/Qwen
// streaming behavior where the upstream emits one delta per active slot per
// chunk in stable index order.
func (a *ToolCallAccumulator) Apply(deltas []api.ToolCall) {
	for i, delta := range deltas {
		a.ApplyAt(i, delta)
	}
}

// ApplyAt merges a single delta into the addressed slot. Use this when the
// upstream provides explicit indices (e.g. via a wrapper that decodes
// OpenAI's `index` field) instead of relying on positional order.
func (a *ToolCallAccumulator) ApplyAt(slot int, delta api.ToolCall) {
	cur, ok := a.bySlot[slot]
	if !ok {
		cur = &api.ToolCall{Type: "function"}
		a.bySlot[slot] = cur
		if slot > a.maxIdx {
			a.maxIdx = slot
		}
	}
	if delta.ID != "" {
		cur.ID = delta.ID
	}
	if delta.Type != "" {
		cur.Type = delta.Type
	}
	if delta.Function.Name != "" {
		cur.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		cur.Function.Arguments = mergeToolArguments(cur.Function.Arguments, delta.Function.Arguments)
	}
	if delta.ExtraContent != nil {
		cur.ExtraContent = delta.ExtraContent
	}
}

// Snapshot returns the assembled tool calls in slot order.
func (a *ToolCallAccumulator) Snapshot() []api.ToolCall {
	if len(a.bySlot) == 0 {
		return nil
	}
	out := make([]api.ToolCall, 0, a.maxIdx+1)
	for i := 0; i <= a.maxIdx; i++ {
		if c, ok := a.bySlot[i]; ok {
			out = append(out, *c)
		}
	}
	return out
}

// Empty reports whether any tool-call deltas have been applied.
func (a *ToolCallAccumulator) Empty() bool { return len(a.bySlot) == 0 }

// mergeToolArguments combines the previously-accumulated tool_call arguments
// with an incoming delta. Most OpenAI-compatible providers stream genuine
// incremental JSON fragments (`{"que` → `ry":"` → `foo"}`), where simple
// concatenation is correct. A handful of providers — Google's Gemini
// OpenAI-compat layer being the poster child — instead re-send the *full*
// arguments JSON in every delta ("snapshot mode"). Naïve concatenation
// there produces `{"query":"foo"}{"query":"foo"}` and the downstream tool
// invocation fails with `invalid character '{' after top-level value`.
//
// Strategy: try to decode the accumulated buffer as JSON. If it parses,
// the buffer already represents a complete argument object, so a new
// delta that also parses on its own is a snapshot — replace the buffer
// with the delta. Otherwise fall back to concatenation (incremental mode).
func mergeToolArguments(current, delta string) string {
	if current == "" {
		return delta
	}
	if !isCompleteJSONValue(current) {
		return current + delta
	}
	if isCompleteJSONValue(delta) {
		// Both sides are parseable → snapshot. Prefer the longer snapshot
		// when they differ in length (providers occasionally reorder keys
		// on repeat but usually extend the payload).
		if len(delta) >= len(current) {
			return delta
		}
		return current
	}
	// Current is complete but delta isn't — provider likely started a
	// fresh incremental stream after a snapshot. Replace with delta so
	// subsequent concatenation yields valid JSON.
	return delta
}

// isCompleteJSONValue reports whether s is a self-contained JSON value.
// Uses json.Decoder so we can detect the "trailing data" case (concatenated
// snapshots) explicitly — a strict json.Unmarshal would silently reject
// those with the same error that triggers the tool_call bug we're fixing.
func isCompleteJSONValue(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return false
	}
	// Anything trailing after the first value means it isn't a single
	// complete value (likely a concatenated snapshot).
	return !dec.More()
}

// StreamAggregator collects deltas from a single upstream stream into a
// non-streaming-shaped ChatResponse. It also tracks finish reason, usage,
// and the synthesized assistant message that the agentic loop appends to
// the message history when a `tool_calls` completion is observed.
//
// Callers should treat the aggregator as single-iteration: instantiate a
// fresh one per agentic loop iteration so the message that gets appended
// to upstream history reflects only that iteration's output.
type StreamAggregator struct {
	id, model, provider, sysFingerprint string
	created                             int64

	role               string
	content            strings.Builder
	reasoning          strings.Builder
	reasoningDetails   []api.ReasoningDetail
	annotations        []interface{}
	toolCalls          *ToolCallAccumulator
	usage              *api.ResponseUsage
	finishReason       string
	nativeFinishReason string
}

func NewStreamAggregator() *StreamAggregator {
	return &StreamAggregator{toolCalls: NewToolCallAccumulator()}
}

// Apply folds a single upstream chunk into the aggregator state. Safe to call
// with a nil chunk (no-op) so callers don't need defensive nil checks.
func (a *StreamAggregator) Apply(chunk *api.ChatResponse) {
	if chunk == nil {
		return
	}
	if chunk.ID != "" {
		a.id = chunk.ID
	}
	if chunk.Model != "" {
		a.model = chunk.Model
	}
	if chunk.SystemFingerprint != "" {
		a.sysFingerprint = chunk.SystemFingerprint
	}
	if chunk.Created != 0 {
		a.created = chunk.Created
	}
	if chunk.Usage != nil {
		// Replace, not merge: providers either send incremental usage or one
		// final cumulative chunk. Replacement preserves the latest accurate view.
		a.usage = chunk.Usage
	}

	for i := range chunk.Choices {
		ch := &chunk.Choices[i]
		if ch.FinishReason != "" {
			a.finishReason = ch.FinishReason
		}
		if ch.NativeFinishReason != "" {
			a.nativeFinishReason = ch.NativeFinishReason
		}
		if ch.Delta == nil {
			continue
		}
		if ch.Delta.Role != "" {
			a.role = ch.Delta.Role
		}
		if ch.Delta.Content.Text != "" {
			a.content.WriteString(ch.Delta.Content.Text)
		}
		if ch.Delta.Reasoning != "" {
			a.reasoning.WriteString(ch.Delta.Reasoning)
		}
		if len(ch.Delta.ReasoningDetails) > 0 {
			a.reasoningDetails = append(a.reasoningDetails, ch.Delta.ReasoningDetails...)
		}
		if len(ch.Delta.ToolCalls) > 0 {
			a.toolCalls.Apply(ch.Delta.ToolCalls)
		}
		if len(ch.Delta.Annotations) > 0 {
			a.annotations = append(a.annotations, ch.Delta.Annotations...)
		}
	}
}

// AssistantMessage materialises the aggregated assistant turn for re-feeding
// into the agentic loop. Reasoning is preserved verbatim because some
// providers (Anthropic) require the full reasoning chain on the assistant
// turn that precedes a tool result.
func (a *StreamAggregator) AssistantMessage() api.ChatMessage {
	role := a.role
	if role == "" {
		role = "assistant"
	}
	msg := api.ChatMessage{
		Role:      role,
		Content:   api.Content{Text: a.content.String()},
		Reasoning: a.reasoning.String(),
		ToolCalls: a.toolCalls.Snapshot(),
	}
	if len(a.reasoningDetails) > 0 {
		msg.ReasoningDetails = append([]api.ReasoningDetail(nil), a.reasoningDetails...)
	}
	if len(a.annotations) > 0 {
		msg.Annotations = a.annotations
	}
	return msg
}

// ReasoningDetails returns the accumulated typed reasoning blocks. Safe to
// call concurrently with Apply only if the caller owns the aggregator; the
// pipeline uses one aggregator per iteration and never shares it.
func (a *StreamAggregator) ReasoningDetails() []api.ReasoningDetail {
	if len(a.reasoningDetails) == 0 {
		return nil
	}
	return append([]api.ReasoningDetail(nil), a.reasoningDetails...)
}

// FinalResponse produces a non-streaming-shaped ChatResponse representing the
// fully-assembled output, suitable for `Service.Chat` to return verbatim.
func (a *StreamAggregator) FinalResponse() *api.ChatResponse {
	msg := a.AssistantMessage()
	return &api.ChatResponse{
		ID:                a.id,
		Model:             a.model,
		SystemFingerprint: a.sysFingerprint,
		Object:            "chat.completion",
		Created:           a.created,
		Choices: []api.Choice{{
			Index:              0,
			Message:            &msg,
			FinishReason:       a.finishReason,
			NativeFinishReason: a.nativeFinishReason,
		}},
		Usage: a.usage,
	}
}

func (a *StreamAggregator) FinishReason() string      { return a.finishReason }
func (a *StreamAggregator) Usage() *api.ResponseUsage { return a.usage }
func (a *StreamAggregator) ID() string                { return a.id }
func (a *StreamAggregator) Model() string             { return a.model }
func (a *StreamAggregator) ToolCalls() []api.ToolCall { return a.toolCalls.Snapshot() }
