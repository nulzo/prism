package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/core/ports"
)

type Handler struct {
	service ports.RouterService
}

func (h *Handler) HandleListModels(c *gin.Context) {
	filter := ports.ModelFilter{
		Provider: c.Query("provider"),
		ID:       c.Query("id"),
		Modality: c.Query("modality"),
		OwnedBy:  c.Query("owned_by"),
	}

	models, err := h.service.ListAllModels(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}
