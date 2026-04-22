package extension

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nulzo/model-router-api/pkg/api"
)

// Extension is a router-native, provider-agnostic tool that the gateway
// executes on behalf of the model. Two identities are deliberately
// distinct:
//
//   - ID()       – stable registry / config identifier, namespaced freely
//     (e.g. "prism:web_search"). This is what clients send
//     to enable the extension; it never crosses the wire
//     to the LLM provider.
//
//   - ToolName() – the function name presented to the LLM. MUST match
//     OpenAI's function-name schema (^[a-zA-Z0-9_-]{1,64}$)
//     because several providers (Google Gemini's OpenAI-
//     compat endpoint, OpenAI itself) silently drop tools
//     with illegal names. Defaults to DefaultToolName(ID())
//     which replaces colons with underscores.
//
// BuildTool() fills in Function.Name from ToolName() automatically via
// the helper BuildFunctionTool below, so extension authors rarely need to
// think about the split — they pick an ID and the wire name is derived.
type Extension interface {
	// ID returns the stable registry/config identifier. Clients use this
	// string to enable the extension on a request.
	ID() string
	// ToolName returns the schema-compliant function name the LLM will
	// call. Defaults to DefaultToolName(ID()) via the default helper;
	// extensions may override for branding or aliasing.
	ToolName() string
	// BuildTool returns the tool definition the LLM sees for this
	// request's configuration.
	BuildTool(config api.ExtensionConfig) (api.Tool, error)
	// Execute runs the extension with JSON-encoded arguments and the
	// request-level extension configuration. The returned string is
	// appended to the conversation as a tool-role message, so keep it
	// concise and JSON-encoded where practical.
	Execute(ctx context.Context, config api.ExtensionConfig, args []byte) (string, error)
}

// DefaultToolName derives an OpenAI-compliant function name from a
// registry ID. Namespacing separators ('.', ':') are replaced with '_'
// because OpenAI's function-name validator rejects them, and Gemini's
// OpenAI-compat shim silently refuses to call tools whose names fail
// that validator. Any other disallowed character is also mapped to '_'
// so new extension IDs can't accidentally break tool calling.
func DefaultToolName(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// Registry manages the set of extensions available to the gateway.
// Registrations are keyed by Extension.ID() so clients enable tools by
// their stable, namespaced identifier regardless of how the wire tool
// name is derived.
type Registry struct {
	mu         sync.RWMutex
	extensions map[string]Extension
}

func NewRegistry() *Registry {
	return &Registry{extensions: make(map[string]Extension)}
}

// Register adds an extension to the registry, keyed by its ID().
// Panics on duplicate ID or empty ID because registration happens at
// startup and silent collisions would be catastrophic.
func (r *Registry) Register(e Extension) {
	id := e.ID()
	if id == "" {
		panic("extension: empty ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.extensions[id]; exists {
		panic(fmt.Sprintf("extension: duplicate ID %q", id))
	}
	r.extensions[id] = e
}

// Get looks up an extension by its registry ID.
func (r *Registry) Get(id string) (Extension, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.extensions[id]
	if !ok {
		return nil, fmt.Errorf("extension not found: %s", id)
	}
	return e, nil
}

// GetAll returns every registered extension. Order is non-deterministic.
func (r *Registry) GetAll() []Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exts := make([]Extension, 0, len(r.extensions))
	for _, e := range r.extensions {
		exts = append(exts, e)
	}
	return exts
}
