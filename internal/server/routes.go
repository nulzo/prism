package server

import (
	"github.com/nulzo/model-router-api/internal/server/middleware"
	v1 "github.com/nulzo/model-router-api/internal/server/v1"
)

func (s *Server) SetupRoutes() {
	// 1. Global Middleware
	// s.router.Use(gin.Logger()) // Already used in New() via middleware.Logger?
	// s.router.Use(gin.Recovery()) // Already used in New()

	s.router.Use(middleware.CORS())
	s.router.Use(middleware.ErrorHandler()) // Register the error handler we fixed!

	// 2. Health Check (Public)
	healthHandler := v1.NewHealthHandler()
	s.router.GET("/health", healthHandler.Health)

	// 3. API V1 Group
	api := s.router.Group("/server/v1")
	api.Use(middleware.Auth(s.config.Server.APIKeys)) // Require API Key for everything below
	{
		chatHandler := v1.NewChatHandler(s.service)
		api.POST("/chat/completions", chatHandler.CreateCompletion)

		modelsHandler := v1.NewModelHandler(s.service)
		api.GET("/models", modelsHandler.ListModels)
	}
}
