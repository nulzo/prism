package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type RoutesHandler struct {
	engine *gin.Engine
}

func NewRoutesHandler(engine *gin.Engine) *RoutesHandler {
	return &RoutesHandler{engine: engine}
}

// RouteInfo represents a single route in the API.
type RouteInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// List returns all registered routes.
// GET /routes
func (h *RoutesHandler) List(c *gin.Context) {
	routes := h.engine.Routes()

	result := make([]RouteInfo, 0, len(routes))
	for _, route := range routes {
		result = append(result, RouteInfo{
			Method: route.Method,
			Path:   route.Path,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"routes": result,
		"count":  len(result),
	})
}
