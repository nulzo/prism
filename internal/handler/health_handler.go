package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	startTime time.Time
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{
		startTime: time.Now(),
	}
}

// Health returns the health status and uptime of the API.
//
// This endpoint is used by load balancers and monitoring systems
// to verify the service is running.
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"uptime": time.Since(h.startTime).String(),
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Ready checks if the service is ready to handle requests.
// GET /ready
func (h *HealthHandler) Ready(c *gin.Context) {
	// Add dependency checks here (DB, cache, etc.)
	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
