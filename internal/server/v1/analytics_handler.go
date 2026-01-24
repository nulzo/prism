package v1

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/pkg/api"
)

type AnalyticsHandler struct {
	service analytics.Service
}

func NewAnalyticsHandler(service analytics.Service) *AnalyticsHandler {
	return &AnalyticsHandler{
		service: service,
	}
}

func (h *AnalyticsHandler) GetUsage(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "7")
	days, err := strconv.Atoi(daysStr)
	if err != nil {
		_ = c.Error(api.BadRequestError("Invalid 'days' parameter"))
		return
	}

	stats, err := h.service.GetUsageOverview(c.Request.Context(), days)
	if err != nil {
		_ = c.Error(api.InternalError("Failed to fetch analytics", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   stats,
	})
}
