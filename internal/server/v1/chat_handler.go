package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/pkg/api"
)

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
		// returns RFC compliant error
		_ = c.Error(api.ValidationError(h.validator.ParseError(err)))
		return
	}

	// if we want to stream the response, roll down into streaming
	if req.Stream {
		h.handleStream(c, &req)
		return
	}

	resp, err := h.service.Chat(c.Request.Context(), &req)
	if err != nil {
		// at this point we hit an upstream error, and we should surface it back
		_ = c.Error(api.InternalError("Failed to process chat request", err.Error()))
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, req *api.ChatRequest) {
	// call the gateway (service)
	streamChan, err := h.service.StreamChat(c.Request.Context(), req)
	if err != nil {
		// if this is a domain problem, we should still serialize it properly
		var problem *api.Problem
		if errors.As(err, &problem) {
			c.JSON(problem.Status, problem)
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// set headers for sse
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	// consume the channel and flush to http
	c.Stream(func(w io.Writer) bool {
		result, ok := <-streamChan
		if !ok {
			// channel is closed
			_, err := io.WriteString(w, "data: [DONE]\n\n")
			if err != nil {
				return false
			}
			return false
		}

		if result.Err != nil {
			errResp := api.ChatResponse{
				Choices: []api.Choice{{
					FinishReason: "error",
					Error:        &api.ErrorResponse{Message: result.Err.Error()},
				}},
			}
			data, _ := json.Marshal(errResp)
			_, err := fmt.Fprintf(w, "data: %s\n\n", data)
			if err != nil {
				return false
			}
			// if there's an error we will stop streaming
			return false
		}

		if result.Response != nil {
			data, err := json.Marshal(result.Response)
			if err == nil {
				_, err := fmt.Fprintf(w, "data: %s\n\n", data)
				return err == nil
			}
		}

		return true
	})
}
