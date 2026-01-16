package router

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/nulzo/model-router-api/internal/config"
	"github.com/nulzo/model-router-api/internal/core/ports"
	"github.com/nulzo/model-router-api/internal/handler"
	"github.com/nulzo/model-router-api/internal/middleware"
)

type Router struct {
	config        *config.Config
	logger        *zap.Logger
	routerService ports.RouterService
	healthHandler *handler.HealthHandler
	configHandler *handler.ConfigHandler
}

func New(cfg *config.Config, logger *zap.Logger, routerService ports.RouterService) *Router {
	return &Router{
		config:        cfg,
		logger:        logger,
		routerService: routerService,
		healthHandler: handler.NewHealthHandler(),
		configHandler: handler.NewConfigHandler(cfg),
	}
}

func (r *Router) Setup() *gin.Engine {

	if r.config.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	engine.Use(gin.Recovery())
	engine.Use(middleware.CORS())
	engine.Use(middleware.Logger(r.logger))
	engine.Use(middleware.Tracing("model-router-api"))

	rateLimiter := middleware.NewRateLimiter(
		r.config.RateLimit.RequestsPerSecond,
		r.config.RateLimit.Burst,
		r.logger,
	)

	engine.GET("/health", r.healthHandler.Health)
	engine.GET("/ready", r.healthHandler.Ready)
	engine.GET("/config", r.configHandler.Get)

	routesHandler := handler.NewRoutesHandler(engine)
	engine.GET("/routes", routesHandler.List)

	v1Group := engine.Group("/v1")
	v1Group.Use(rateLimiter.Middleware())
	// v1Group.Use(middleware.AuthMiddleware(...)) // Add Auth here if needed

	h := handler.NewHandler(r.routerService)

	v1Group.POST("/chat/completions", h.HandleChatCompletions)
	v1Group.GET("/models", h.HandleListModels)

	return engine
}
