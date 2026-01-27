package server

import (
	"github.com/nulzo/model-router-api/internal/server/middleware"
	v1 "github.com/nulzo/model-router-api/internal/server/v1"
)

func (s *Server) SetupRoutes() {

	s.router.Use(middleware.CORS())
	s.router.Use(middleware.ErrorHandler())

	healthHandler := v1.NewHealthHandler()
	s.router.GET("/health", healthHandler.Health)
	s.router.GET("/routes", v1.NewRoutesHandler(s.router).List)
	s.router.GET("/config", v1.NewConfigHandler(s.config).Get)

	api := s.router.Group("/api/v1")

	if s.config.Server.AuthEnabled {
		api.Use(middleware.Auth(s.repo, s.config.Server.APIKeys))
	}

	chatHandler := v1.NewChatHandler(s.service, s.validator)
	api.POST("/chat/completions", chatHandler.CreateCompletion)

	modelsHandler := v1.NewModelHandler(s.service)
	api.GET("/models", modelsHandler.ListModels)

	analyticsHandler := v1.NewAnalyticsHandler(s.analytics)
	api.GET("/analytics/usage", analyticsHandler.GetUsage)

}
