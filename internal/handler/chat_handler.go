package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/core/domain"
	"github.com/nulzo/model-router-api/pkg/schema"
)

func (h *Handler) HandleChatCompletions(c *gin.Context) {
	var req schema.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errMap := domain.ParseValidationError(err)
		log.Printf("%s", errMap)
		// returns RFC compliant error
		c.Error(domain.ValidationError(errMap))
		return
	}

	// if we want to stream the response, roll down into streaming
	if req.Stream {
		h.handleStream(c, &req)
		return
	}

	resp, err := h.service.Chat(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleStream(c *gin.Context, req *schema.ChatRequest) {
	// Call the gateway (service)
	streamChan, err := h.service.StreamChat(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Set headers for SSE (Server-Sent Events)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	// Consume the channel and flush to client
	c.Stream(func(w io.Writer) bool {
		result, ok := <-streamChan
		if !ok {
			// Channel closed
			io.WriteString(w, "data: [DONE]\n\n")
			return false
		}

		if result.Err != nil {
			errResp := schema.ChatResponse{
				Choices: []schema.Choice{{
					FinishReason: "error",
					Error:        &schema.ErrorResponse{Message: result.Err.Error()},
				}},
			}
			data, _ := json.Marshal(errResp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			return false // Stop streaming on error
		}

		if result.Response != nil {
			data, err := json.Marshal(result.Response)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				return true
			}
		}

		return true
	})
}
