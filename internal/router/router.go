package router

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"GoGinApi/internal/config"
	"GoGinApi/internal/handler"
	"GoGinApi/internal/middleware"
)

type Router struct {
	config        *config.Config
	logger        *zap.Logger
	healthHandler *handler.HealthHandler
	pingHandler   *handler.PingHandler
	configHandler *handler.ConfigHandler
}

func New(cfg *config.Config, logger *zap.Logger) *Router {
	return &Router{
		config:        cfg,
		logger:        logger,
		healthHandler: handler.NewHealthHandler(),
		pingHandler:   handler.NewPingHandler(),
		configHandler: handler.NewConfigHandler(cfg),
	}
}

func (r *Router) Setup() *gin.Engine {
	// Set Gin mode based on environment
	if r.config.Server.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	// Global middleware (applied to ALL routes)
	engine.Use(gin.Recovery())
	engine.Use(middleware.Tracing("GoGinApi"))
	engine.Use(middleware.Logger(r.logger))

	// Create rate limiter
	rateLimiter := middleware.NewRateLimiter(
		r.config.RateLimit.RequestsPerSecond,
		r.config.RateLimit.Burst,
		r.logger,
	)

	engine.GET("/health", r.healthHandler.Health)
	engine.GET("/ready", r.healthHandler.Ready)
	engine.GET("/config", r.configHandler.Get)

	v1 := engine.Group("/api/v1")
	//v1.Use(middleware.Auth(r.config.Auth.APIKey, r.logger))
	v1.Use(rateLimiter.Middleware())
	{
		v1.GET("/ping", r.pingHandler.Ping)
		v1.POST("/echo", r.pingHandler.Echo)
	}

	routesHandler := handler.NewRoutesHandler(engine)
	engine.GET("/routes", routesHandler.List)

	return engine
}
