package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/pkg/api"
)

// SSE keepalive cadence. OpenRouter sends `: OPENROUTER PROCESSING` on a
// similar interval; we mirror the convention so reverse proxies don't kill
// the connection during long agentic tool loops.
const sseKeepaliveInterval = 15 * time.Second

type ChatHandler struct {
	service   gateway.Service
	validator *validator.Validator
}

func NewChatHandler(service gateway.Service, v *validator.Validator) *ChatHandler {
	return &ChatHandler{
		service:   service,
		validator: v,
	}
}

func (h *ChatHandler) CreateCompletion(c *gin.Context) {
	var req api.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(api.ValidationError(h.validator.ParseError(err)))
		return
	}

	// Generation ID is request-scoped: same value lands in the response body,
	// the X-Generation-Id header, and the analytics record. Echoed from the
	// inbound header (for client-driven correlation) or freshly minted.
	genID := c.GetHeader("X-Generation-Id")
	if genID == "" {
		genID = uuid.NewString()
	}
	c.Writer.Header().Set("X-Generation-Id", genID)
	ctx := gateway.WithGenerationID(c.Request.Context(), genID)

	if req.Stream {
		h.handleStream(c, ctx, &req)
		return
	}

	resp, err := h.service.Chat(ctx, &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, ctx context.Context, req *api.ChatRequest) {
	streamChan, err := h.service.StreamChat(ctx, req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return

		case <-keepalive.C:
			// SSE comment line — clients ignore it but it keeps the TCP
			// connection alive through proxies during long extension calls.
			if _, err := io.WriteString(w, ": prism keepalive\n\n"); err != nil {
				return
			}
			flush()

		case result, ok := <-streamChan:
			if !ok {
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				flush()
				return
			}

			if result.Err != nil {
				if writeStreamError(w, result.Err) != nil {
					return
				}
				flush()
				return
			}
			if result.Response == nil {
				continue
			}
			if writeStreamChunk(w, result.Response) != nil {
				return
			}
			flush()
		}
	}
}

// writeStreamChunk serialises a chat.completion.chunk and writes it as an
// SSE event. Synthetic gateway events (e.g. extension tool execution) are
// distinguished by a named `event:` line so UI clients can subscribe; the
// JSON payload also includes the same data so plain `data:`-only consumers
// (like the OpenAI SDK) still see the chunk.
func writeStreamChunk(w io.Writer, resp *api.ChatResponse) error {
	eventName := classifyChunk(resp)
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if eventName != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", eventName); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// classifyChunk inspects a chunk and returns a named SSE event type when the
// chunk carries gateway-specific signal. Returning "" yields a plain
// `data:`-only event (the default OpenAI shape).
func classifyChunk(resp *api.ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Delta == nil {
		return ""
	}
	for _, ann := range resp.Choices[0].Delta.Annotations {
		m, ok := ann.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); t == gateway.PrismToolEventAnnotationType {
			return gateway.PrismToolEventAnnotationType
		}
	}
	return ""
}

// writeStreamError emits an OpenRouter-shaped mid-stream error chunk. HTTP
// status is already 200 by this point (headers were sent), so the error has
// to ride in-band.
func writeStreamError(w io.Writer, err error) error {
	chunk := api.ChatResponse{
		Object: "chat.completion.chunk",
		Choices: []api.Choice{{
			Index:        0,
			Delta:        &api.ChatMessage{Role: "assistant"},
			FinishReason: "error",
			Error:        streamErrorResponse(err),
		}},
		Error: streamErrorResponse(err),
	}
	data, mErr := json.Marshal(chunk)
	if mErr != nil {
		return mErr
	}
	_, wErr := fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
	return wErr
}

func streamErrorResponse(err error) *api.ErrorResponse {
	var problem *api.Problem
	if errors.As(err, &problem) {
		resp := &api.ErrorResponse{
			Code:    problem.Status,
			Message: problem.Detail,
		}
		if len(problem.Extensions) > 0 {
			resp.Metadata = problem.Extensions
		}
		return resp
	}

	var appErr *api.Error
	if errors.As(err, &appErr) {
		return &api.ErrorResponse{
			Code:    appErr.Code,
			Message: appErr.Message,
		}
	}

	return &api.ErrorResponse{Message: err.Error()}
}
