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
	return &ModelHandler{service: service}
}

func (h *ModelHandler) ListModels(c *gin.Context) {
	filter := api.ModelFilter{
		Provider: c.Query("provider"),
		ID:       c.Query("id"),
		Modality: c.Query("modality"),
		OwnedBy:  c.Query("owned_by"),
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

// RefreshModels triggers an immediate catalog hydrate. Accepts an optional
// `?provider=<id>` query param (repeatable) to scope the refresh to one
// or more providers; without a filter, every registered provider is
// refreshed. Returns the diff summary so operators can see what changed.
func (h *ModelHandler) RefreshModels(c *gin.Context) {
	providers := c.QueryArray("provider")

	result, err := h.service.RefreshCatalog(c.Request.Context(), providers...)
	if err != nil {
		_ = c.Error(api.InternalError("Failed to refresh catalog", err.Error()))
		return
	}

	perProvider := make(map[string]gin.H, len(result.Providers))
	for id, pr := range result.Providers {
		entry := gin.H{
			"models":      pr.Models,
			"duration_ms": pr.Duration.Milliseconds(),
		}
		if pr.Err != nil {
			entry["error"] = pr.Err.Error()
		}
		perProvider[id] = entry
	}

	c.JSON(http.StatusOK, gin.H{
		"total_models": result.TotalModels,
		"added":        result.Added,
		"removed":      result.Removed,
		"updated":      result.Updated,
		"providers":    perProvider,
		"duration_ms":  result.Duration.Milliseconds(),
	})
}
