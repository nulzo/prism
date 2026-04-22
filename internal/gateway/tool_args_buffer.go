package gateway

// ToolArgsBuffer assembles streaming tool-call argument fragments emitted
// by an LLM provider into a single valid JSON value. It is intentionally
// provider-agnostic: every OpenAI-compatible upstream pushes tool_call
// argument fragments through the same `function.arguments` string field,
// but the chunking strategy varies wildly between providers:
//
//  1. Incremental (OpenAI, Anthropic via OpenAI shim, most OSS builds):
//     each delta carries a strict suffix of the in-flight JSON object, so
//     naïve concatenation reconstructs the value correctly.
//
//     deltas: `{"q":"f`, `oo"}`  →  `{"q":"foo"}`
//
//  2. Full-snapshot (Gemini's OpenAI-compat shim on Vertex/Developer
//     API, some Qwen and Moonshot configurations): each delta re-sends
//     the entire argument object from the beginning. Naïve concat
//     produces `{…}{…}` and downstream json.Unmarshal fails with
//     `invalid character '{' after top-level value`.
//
//     deltas: `{"q":"foo"}`, `{"q":"foo"}`  →  `{"q":"foo"}`
//
//  3. Progressive snapshots (a Gemini variant where each delta is a
//     longer snapshot than the previous one):
//
//     deltas: `{"q":"f"}`, `{"q":"fo"}`, `{"q":"foo"}`  →  `{"q":"foo"}`
//
//  4. Multi-snapshot-in-one-delta (a pathological but observed Gemini
//     mode where one delta contains several repeated snapshots):
//
//     deltas: `{"q":"foo"}{"q":"foo"}`  →  `{"q":"foo"}`
//
//  5. Hybrid mode (snapshot followed by a fresh incremental restart on
//     a different value — rare, but observed when Gemini decides to
//     amend its tool call mid-stream):
//
//     deltas: `{"q":"a"}`, `{"q":"bb`, `"}`  →  `{"q":"bb"}`
//
// The buffer implements a byte-at-a-time JSON structural scanner that
// tracks bracket depth and string-literal state. Every time the scanner
// enters depth 0 via a top-level `{` or `[` while a previous value has
// already closed, it resets the buffer and starts recording the new
// value. At any point, String() returns either the most-recent closed
// top-level value (clean case) or the in-flight buffer (partial case),
// so callers that hold the result between chunks always see a
// best-effort representation of what the provider intends.
//
// The scanner is permissive: malformed bytes that can't be explained by
// either streaming mode (e.g. garbage before the first `{`) are
// dropped. This matches OpenRouter's behavior of silently normalizing
// provider deltas and avoids propagating upstream bugs to the caller.
//
// ToolArgsBuffer is NOT safe for concurrent use; callers hold one
// buffer per tool-call slot inside ToolCallAccumulator.
type ToolArgsBuffer struct {
	// buf holds bytes for the current top-level JSON value. It is
	// reset every time we detect a snapshot restart.
	buf []byte

	// depth is the current JSON structural nesting level: 0 means we're
	// outside any value, >0 means we're inside N levels of `{` / `[`.
	depth int

	// inString reports whether the scanner is currently inside a JSON
	// string literal. Structural characters like `{}[]` inside strings
	// must NOT affect depth tracking.
	inString bool

	// escape reports whether the previous character inside a string
	// literal was an unescaped `\`; the next character is then treated
	// literally regardless of what it would otherwise mean.
	escape bool

	// complete becomes true the moment depth returns to 0 after being
	// >0 at least once. It stays true until the scanner sees a new
	// top-level opening brace that triggers a buffer reset, OR a valid
	// continuation byte (whitespace) that it still appends.
	complete bool
}

// NewToolArgsBuffer returns an empty buffer ready to receive fragments.
func NewToolArgsBuffer() *ToolArgsBuffer { return &ToolArgsBuffer{} }

// Write ingests the next fragment from an upstream delta. Safe to call
// with an empty string (no-op). Each byte is routed through the
// scanner; malformed input that can't be explained by the recognized
// streaming modes is dropped on the floor rather than propagated as
// invalid JSON.
func (b *ToolArgsBuffer) Write(s string) {
	if s == "" {
		return
	}
	for i := 0; i < len(s); i++ {
		b.feed(s[i])
	}
}

// String returns the current best-effort view of the accumulated
// argument value. When the last top-level value is closed, it returns
// that value verbatim. When the scanner is mid-value, it returns the
// in-flight bytes (invalid JSON on its own, but meaningful for
// logging and for providers that legitimately mid-stream). Callers
// that need strict validation should feed the result back into
// json.Unmarshal.
func (b *ToolArgsBuffer) String() string { return string(b.buf) }

// Empty reports whether the buffer has accumulated nothing yet. Used by
// the accumulator to avoid producing synthetic empty ToolCall entries.
func (b *ToolArgsBuffer) Empty() bool { return len(b.buf) == 0 }

// feed processes a single byte. Kept as a hot-path method so the tight
// loop in Write can inline it.
func (b *ToolArgsBuffer) feed(c byte) {
	if b.inString {
		b.buf = append(b.buf, c)
		switch {
		case b.escape:
			b.escape = false
		case c == '\\':
			b.escape = true
		case c == '"':
			b.inString = false
		}
		return
	}

	switch c {
	case '"':
		if b.depth == 0 {
			// Top-level bare string value — unusual for OpenAI tool
			// args (schema almost always mandates an object), but
			// legal JSON. Treat as a fresh snapshot if we already
			// completed one.
			if b.complete {
				b.reset()
			}
		}
		b.buf = append(b.buf, c)
		b.inString = true
	case '{', '[':
		if b.depth == 0 {
			// New top-level value begins. If a prior value already
			// closed in this stream, drop it — this is a snapshot
			// restart or a second snapshot in the same delta. We
			// keep only the newest representation.
			if b.complete {
				b.reset()
			}
		}
		b.depth++
		b.buf = append(b.buf, c)
	case '}', ']':
		b.buf = append(b.buf, c)
		b.depth--
		if b.depth == 0 {
			b.complete = true
		} else if b.depth < 0 {
			// Unbalanced close — treat the stream as corrupt and
			// reset. The next `{` / `[` will start fresh.
			b.resetAll()
		}
	case ' ', '\t', '\n', '\r':
		// Whitespace is only meaningful inside an in-flight value.
		// Between the end of a snapshot and the next one, it's
		// cosmetic and we'd rather drop it than emit
		// `{...} {...}`-style output if the scanner decides later
		// that the first value was a snapshot restart target.
		if b.depth > 0 {
			b.buf = append(b.buf, c)
		}
	default:
		// Structural characters inside a value (commas, colons),
		// numbers, literal booleans / null bytes. We append only
		// when we're actively building a value; stray bytes at depth
		// 0 with no prior opener are ignored so that garbled
		// upstreams don't corrupt the buffer.
		if b.depth > 0 || (!b.complete && len(b.buf) > 0) {
			b.buf = append(b.buf, c)
		}
	}
}

// reset clears the buffer while preserving that we're about to start a
// new value. The `complete` flag is cleared because the next `{` or `[`
// has already been consumed by the caller's depth++ path.
func (b *ToolArgsBuffer) reset() {
	b.buf = b.buf[:0]
	b.complete = false
}

// resetAll is the stronger reset used on unbalanced input. It puts the
// scanner back into its initial state so the next complete-looking
// byte sequence can start fresh.
func (b *ToolArgsBuffer) resetAll() {
	b.buf = b.buf[:0]
	b.depth = 0
	b.inString = false
	b.escape = false
	b.complete = false
}
