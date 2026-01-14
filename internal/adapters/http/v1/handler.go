package v1

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Handler struct {
	service ports.RouterService
}

func NewHandler(service ports.RouterService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	router.POST("/chat/completions", h.HandleChatCompletions)
	router.GET("/models", h.HandleListModels)
}

func (h *Handler) HandleListModels(c *gin.Context) {
	models, err := h.service.ListAllModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

func (h *Handler) HandleChatCompletions(c *gin.Context) {
	var req schema.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
		return
	}

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model is required"})
		return
	}

	p, err := h.service.GetProviderForModel(req.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if req.Stream {
		h.handleStream(c, p, &req)
		return
	}

	resp, err := p.Chat(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleStream(c *gin.Context, p ports.ModelProvider, req *schema.ChatRequest) {
	streamChan, err := p.Stream(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	for result := range streamChan {
		if result.Err != nil {
			errResp := schema.ChatResponse{
				Choices: []schema.Choice{{
					FinishReason: "error",
					Error:        &schema.ErrorResponse{Message: result.Err.Error()},
				}},
			}
			data, _ := json.Marshal(errResp)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()
			return
		}

		if result.Response != nil {
			data, err := json.Marshal(result.Response)
			if err == nil {
				fmt.Fprintf(c.Writer, "data: %s\n\n", data)
				c.Writer.Flush()
			}
		}
	}

	io.WriteString(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
