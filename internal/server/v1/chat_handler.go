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
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, req *api.ChatRequest) {
	// call the gateway (service)
	streamChan, err := h.service.StreamChat(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
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
		select {
		case <-c.Request.Context().Done():
			// Client disconnected, stop processing
			return false
		case result, ok := <-streamChan:
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
						Error:        streamErrorResponse(result.Err),
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
		}

		return true
	})
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
