package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/pkg/api"
)

type ModelHandler struct {
	service gateway.Service
}

func NewModelHandler(service gateway.Service) *ModelHandler {
	return &ModelHandler{
		service: service,
	}
}

func (h *ModelHandler) ListModels(c *gin.Context) {
	filter := api.ModelFilter{
		Provider:       c.Query("provider"),
		InputModality:  c.Query("input_modalities"),
		OutputModality: c.Query("output_modalities"),
	}

	// fetch all models/providers from underlying services
	models, err := h.service.ListAllModels(c.Request.Context(), filter)
	if err != nil {
		// throw 500 internal server error
		_ = c.Error(api.InternalError("Failed to list models", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}
