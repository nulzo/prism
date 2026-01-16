package handler

import "github.com/nulzo/model-router-api/internal/core/ports"

// Handler is the central HTTP handler for the application.
// It groups domain-specific handlers that share dependencies.
type Handler struct {
	service ports.RouterService
}

// NewHandler creates a new Handler instance.
func NewHandler(service ports.RouterService) *Handler {
	return &Handler{service: service}
}
