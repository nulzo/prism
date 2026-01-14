package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/provider"
	"github.com/nulzo/model-router-api/internal/router"
	"github.com/nulzo/model-router-api/pkg/schema"
)

type Handler struct {
	router *router.Router
}

func NewHandler(r *router.Router) *Handler {
	return &Handler{router: r}
}

// RegisterRoutes sets up the Gin routes
func (h *Handler) RegisterRoutes(engine *gin.Engine) {
	v1 := engine.Group("/v1")
	{
		v1.POST("/chat/completions", h.HandleChatCompletions)
		v1.GET("/models", h.HandleListModels)
	}
}

func (h *Handler) HandleListModels(c *gin.Context) {
	models, err := h.router.ListModels(c.Request.Context())
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

	// Basic validation
	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model is required"})
		return
	}

	// Get Provider
	p, err := h.router.GetProvider(req.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Streaming logic
	if req.Stream {
		h.handleStream(c, p, &req)
		return
	}

	// Non-streaming logic
	resp, err := p.Chat(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleStream(c *gin.Context, p provider.ModelProvider, req *schema.ChatRequest) {
	// Start Stream
	streamChan, err := p.Stream(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Set Headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	// Stream response
	for result := range streamChan {
		if result.Err != nil {
			// If error occurs mid-stream, we try to send an error object if possible
			// or just close connection. OpenAI sends error in data usually.
			// Construct an error response
			errResp := schema.ChatResponse{
				Choices: []schema.Choice{
					{
						FinishReason: "error",
						Error: &schema.ErrorResponse{
							Message: result.Err.Error(),
						},
					},
				},
			}
			data, _ := json.Marshal(errResp)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()
			return // Stop streaming
		}

		if result.Response != nil {
			data, err := json.Marshal(result.Response)
			if err != nil {
				continue // Skip bad chunks
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()
		}
	}

	// End Stream
	io.WriteString(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
