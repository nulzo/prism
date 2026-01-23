package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/platform/logger"
	"go.uber.org/zap"
)

// LoggerMiddleware is a Gin middleware that uses our global zap logger
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)

		status := c.Writer.Status()

		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("ip", c.ClientIP()),
			zap.Duration("latency", latency),
		}

		if query != "" {
			fields = append(fields, zap.String("query", query))
		}

		if ua := c.Request.UserAgent(); ua != "" {
			fields = append(fields, zap.String("user-agent", ua))
		}

		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		msg := path

		switch {
		case status >= 500:
			logger.Error(msg, fields...)
		case status >= 400:
			logger.Warn(msg, fields...)
		default:
			logger.Info(msg, fields...)
		}
	}
}
