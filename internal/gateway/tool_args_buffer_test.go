package gateway

import "testing"

// Every table entry describes one streaming sequence we've either seen in
// the wild from a concrete provider or anticipate a new provider will
// produce. The want value is the final JSON the buffer should surface to
// downstream json.Unmarshal — always a single self-contained JSON value
// that round-trips through a strict decoder.
func TestToolArgsBuffer(t *testing.T) {
	cases := []struct {
		name   string
		deltas []string
		want   string
	}{
		{
			name:   "empty stream",
			deltas: []string{},
			want:   "",
		},
		{
			// OpenAI, Anthropic-via-OpenAI-shim, most OSS: each delta
			// is a strict suffix of the in-flight object.
			name:   "incremental fragments",
			deltas: []string{`{"q":"f`, `oo"}`},
			want:   `{"q":"foo"}`,
		},
		{
			// Gemini OpenAI-compat: every delta re-sends the complete
			// object. Naïve concat would produce `{...}{...}`.
			name:   "pure snapshot repeats",
			deltas: []string{`{"q":"foo"}`, `{"q":"foo"}`, `{"q":"foo"}`},
			want:   `{"q":"foo"}`,
		},
		{
			// Gemini progressive snapshots: each delta is a longer
			// snapshot than the previous one. We keep the latest.
			name:   "progressive snapshots",
			deltas: []string{`{"q":"f"}`, `{"q":"fo"}`, `{"q":"foo"}`},
			want:   `{"q":"foo"}`,
		},
		{
			// Pathological Gemini mode: one delta contains two full
			// snapshots concatenated. This is the exact pattern
			// producing the production `invalid character '{' after
			// top-level value` error in the bug report.
			name:   "multi snapshot in one delta",
			deltas: []string{`{"q":"foo"}{"q":"foo"}`},
			want:   `{"q":"foo"}`,
		},
		{
			// Gemini occasionally amends its tool call mid-stream
			// with a fresh incremental delta from a different start.
			name:   "snapshot then incremental restart",
			deltas: []string{`{"q":"a"}`, `{"q":"bb`, `"}`},
			want:   `{"q":"bb"}`,
		},
		{
			// Nested object arguments — common for tools with rich
			// schemas (e.g. filters, options).
			name:   "nested object fragments",
			deltas: []string{`{"filter":{"typ`, `e":"image",`, `"count":5}}`},
			want:   `{"filter":{"type":"image","count":5}}`,
		},
		{
			// Escaped quotes inside strings must not be treated as
			// string boundaries by the scanner.
			name:   "escaped quotes in string",
			deltas: []string{`{"q":"he said \"hi\""}`},
			want:   `{"q":"he said \"hi\""}`,
		},
		{
			// A single `{` split across many 1-byte deltas stress-
			// tests the byte-at-a-time feed loop.
			name:   "byte at a time",
			deltas: strs(`{"q":"hello"}`),
			want:   `{"q":"hello"}`,
		},
		{
			// Arrays are legal JSON tool arguments in principle (some
			// schemas use them for list params). The scanner should
			// handle `[` / `]` symmetrically with `{` / `}`.
			name:   "array argument",
			deltas: []string{`[1,2,`, `3]`},
			want:   `[1,2,3]`,
		},
		{
			// Three full snapshots concatenated in a single delta —
			// pushes the snapshot-restart logic through N iterations
			// inside one feed() loop.
			name:   "three snapshots in one delta",
			deltas: []string{`{"q":"a"}{"q":"b"}{"q":"c"}`},
			want:   `{"q":"c"}`,
		},
		{
			// Between snapshots Gemini sometimes pads whitespace. It
			// must be dropped at depth 0 so the output remains a
			// single clean value.
			name:   "whitespace between snapshots",
			deltas: []string{"{\"q\":\"a\"}\n{\"q\":\"b\"}"},
			want:   `{"q":"b"}`,
		},
		{
			// Bracket characters inside a string literal must not
			// affect the depth counter.
			name:   "brackets inside strings",
			deltas: []string{`{"q":"a }{ b"}`},
			want:   `{"q":"a }{ b"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := NewToolArgsBuffer()
			for _, d := range tc.deltas {
				buf.Write(d)
			}
			got := buf.String()
			if got != tc.want {
				t.Fatalf("buffer mismatch:\n  got:  %q\n  want: %q", got, tc.want)
			}
		})
	}
}

// TestToolArgsBuffer_ResetOnUnbalanced verifies that an unbalanced close
// (more `}` than `{`) resets the scanner so a subsequent well-formed
// value is still captured cleanly rather than corrupting the buffer.
func TestToolArgsBuffer_ResetOnUnbalanced(t *testing.T) {
	buf := NewToolArgsBuffer()
	buf.Write(`}}}{"ok":true}`)
	if got, want := buf.String(), `{"ok":true}`; got != want {
		t.Fatalf("unbalanced recovery failed:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestSanitizeArguments_Fallback covers the defense-in-depth call-site
// helper that extension execution uses. Verifies that the two most
// important corner cases — empty input and runaway malformed input —
// degrade to an empty-object default instead of propagating a decode
// error to the model.
func TestSanitizeArguments_Fallback(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "{}"},
		{"   ", "{}"},
		{`{"q":"foo"}`, `{"q":"foo"}`},
		{`{"q":"foo"}{"q":"foo"}`, `{"q":"foo"}`},
		{`not json`, "{}"}, // garbage input degrades to empty object
	}
	for _, tc := range cases {
		got := SanitizeArguments(tc.in)
		if got != tc.want {
			t.Fatalf("SanitizeArguments(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// strs splits s into single-byte deltas. Used by the `byte at a time`
// test case to stress the scanner's per-byte feed path.
func strs(s string) []string {
	out := make([]string, 0, len(s))
	for i := 0; i < len(s); i++ {
		out = append(out, string(s[i]))
	}
	return out
}
