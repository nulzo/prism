package gateway

import "context"

// contextKey is unexported to prevent collisions with other packages'
// context values. Use the typed accessors below.
type contextKey int

const (
	ctxKeyGenerationID contextKey = iota + 1
)

// WithGenerationID stamps a request-scoped generation id on the context.
// The HTTP handler sets this from `X-Generation-Id` (or a freshly minted UUID)
// so the same id appears in the response body, the response header, and the
// analytics record.
func WithGenerationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyGenerationID, id)
}

// GenerationID returns the request-scoped generation id, or "" if absent.
func GenerationID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyGenerationID).(string)
	return v
}
