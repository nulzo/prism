package server

import (
	"fmt"
	"net/http"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/analytics"
	"github.com/nulzo/model-router-api/internal/cli"
	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/gateway"
	"github.com/nulzo/model-router-api/internal/server/validator"
	"github.com/nulzo/model-router-api/internal/store"
	"go.uber.org/zap"
)

type Server struct {
	router    *gin.Engine
	config    *config.Config
	logger    *zap.Logger
	repo      store.Repository
	service   gateway.Service
	analytics analytics.Service
	validator *validator.Validator
}

func New(cfg *config.Config, logger *zap.Logger, repo store.Repository, service gateway.Service, analytics analytics.Service, v *validator.Validator) *Server {

	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()

	engine.Use(ginzap.RecoveryWithZap(logger, true))

	engine.Use(ginzap.Ginzap(logger, time.RFC3339, true))

	s := &Server{
		router:    engine,
		repo:      repo,
		service:   service,
		analytics: analytics,
		logger:    logger,
		config:    cfg,
		validator: v,
	}

	s.SetupRoutes()

	// we supressed gin debug but we can manually log the routes
	// nicely on startup.
	logger.Debug(cli.BoldText("Available Endpoints: "))
	for _, route := range engine.Routes() {
		msg := fmt.Sprintf("%s  %s%-6s%s",
			cli.BoldCode, route.Method, cli.ResetCode,
			route.Path,
		)
		logger.Debug(msg)
	}
	logger.Debug(cli.BoldText(fmt.Sprintf("Total Endpoints: %d", len(engine.Routes()))))

	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}
