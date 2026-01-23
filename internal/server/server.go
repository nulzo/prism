package server

import (
	"fmt"
	"net/http"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"go.uber.org/zap"
)

type Server struct {
	router  *gin.Engine
	config  *config.Config
	logger  *zap.Logger
	service gateway.Service
}

func New(cfg *config.Config, logger *zap.Logger, service gateway.Service) *Server {

	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()

	engine.Use(ginzap.RecoveryWithZap(logger, true))

	engine.Use(ginzap.Ginzap(logger, time.RFC3339, true))

	s := &Server{
		router:  engine,
		service: service,
		logger:  logger,
		config:  cfg,
	}

	s.SetupRoutes()

	// we supressed gin debug but we can manually log the routes
	// nicely on startup.
	for _, route := range engine.Routes() {
		msg := fmt.Sprintf("%s➜%s  %s%-6s%s %s",
			cli.Blue, cli.Reset,
			cli.Bold, route.Method, cli.Reset,
			route.Path,
		)
		logger.Debug(msg)
	}

	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}
