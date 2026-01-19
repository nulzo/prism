package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nulzo/model-router-api/internal/config"
)

type ConfigHandler struct {
	config *config.Config
}

func NewConfigHandler(cfg *config.Config) *ConfigHandler {
	return &ConfigHandler{config: cfg}
}

// Get returns the current application configuration.
//
// GET /config
func (h *ConfigHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"config": h.config,
	})
}
