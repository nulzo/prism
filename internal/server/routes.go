package server

import (
	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/server/middleware"
)

func (s *Server) SetupRoutes() {
	// 1. Global Middleware
	s.router.Use(gin.Logger())
	s.router.Use(gin.Recovery())
	s.router.Use(middleware.Cors())

	// 2. Health Check (Public)
	s.router.GET("/health", s.handleHealth)

	// 3. API V1 Group
	api := s.router.Group("/server/v1")
	api.Use(middleware.Auth()) // Require API Key for everything below
	{
		// Map the handler function to the path
		// Note: We use a closure or method reference to pass dependencies if needed,
		// but since 's' holds the service, we can define handlers as methods on *Server
		// OR (cleaner approach): Delegate to a sub-handler

		chatHandler := v1.NewChatHandler(s.service)
		api.POST("/chat/completions", chatHandler.CreateCompletion)

		modelsHandler := v1.NewModelHandler(s.service)
		api.GET("/models", modelsHandler.ListModels)
	}
}
