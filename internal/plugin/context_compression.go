package plugin

import (
	"context"
)

// ContextCompressionPlugin is a simple plugin that truncates the message history
// if it exceeds a certain length, preserving the system prompt.
type ContextCompressionPlugin struct {
	MaxMessages int
}

func NewContextCompressionPlugin(maxMessages int) *ContextCompressionPlugin {
	return &ContextCompressionPlugin{MaxMessages: maxMessages}
}

func (p *ContextCompressionPlugin) ID() string {
	return "context-compression"
}

func (p *ContextCompressionPlugin) PreProcess(ctx context.Context, pCtx *Context) error {
	if len(pCtx.Request.Messages) <= p.MaxMessages {
		return nil // Nothing to compress
	}

	// Keep the first message (usually system prompt) and the last (MaxMessages - 1) messages
	compressed := pCtx.Request.Messages[:1]
	tail := pCtx.Request.Messages[len(pCtx.Request.Messages)-(p.MaxMessages-1):]
	compressed = append(compressed, tail...)

	pCtx.Request.Messages = compressed
	return nil
}

func (p *ContextCompressionPlugin) PostProcess(ctx context.Context, pCtx *Context) error {
	// No post-processing needed for compression
	return nil
}
