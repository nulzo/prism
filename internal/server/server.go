package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/server/middleware"
	"go.uber.org/zap"
)

type Server struct {
	router  *gin.Engine
	config  *config.Config
	logger  *zap.Logger
	service gateway.Service
}

func New(cfg *config.Config, logger *zap.Logger, service gateway.Service) *Server {

	if cfg.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	engine.Use(gin.Recovery())
	engine.Use(middleware.Logger(logger))

	s := &Server{
		router:  engine,
		service: service,
		logger:  logger,
		config:  cfg,
	}

	s.SetupRoutes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}