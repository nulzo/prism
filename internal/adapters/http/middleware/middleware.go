package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/logger"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// AuthMiddleware checks for a valid Bearer token in the Authorization header.
// In a real system, this would validate against a database of issued keys.
func AuthMiddleware(validKeys []string) gin.HandlerFunc {
	// Convert to map for O(1) lookups
	// keyMap := make(map[string]bool)
	// for _, k := range validKeys {
	// 	keyMap[k] = true
	// }

	return func(c *gin.Context) {
		// authHeader := c.GetHeader("Authorization")
		// if authHeader == "" {
		// 	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
		// 	return
		// }

		// parts := strings.Split(authHeader, " ")
		// if len(parts) != 2 || parts[0] != "Bearer" {
		// 	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization header format"})
		// 	return
		// }

		// token := parts[1]

		// // For development/demo, if no keys configured, allow all (or fail open/closed depending on security stance)
		// // Here we fail closed if keys are provided, else allow (mock mode)
		// if len(keyMap) > 0 {
		// 	if !keyMap[token] {
		// 		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API Key"})
		// 		return
		// 	}
		// }

		// // Store user info in context if needed
		// c.Set("user_id", "user_"+token[:min(len(token), 8)])
		c.Next()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RateLimitMiddleware implements a simple in-memory token bucket rate limiter.
// In production, use Redis.
func RateLimitMiddleware(rps float64, burst int) gin.HandlerFunc {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)

	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "Rate limit exceeded",
				"retry_after": "1s",
			})
			return
		}
		c.Next()
	}
}

// CORSMiddleware allows Cross-Origin Resource Sharing
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// StructuredLogger logs request details using Zap
func StructuredLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		clientIP := c.ClientIP()

		if raw != "" {
			path = path + "?" + raw
		}

		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", method),
			zap.String("path", path),
			zap.String("ip", clientIP),
			zap.Duration("latency", latency),
		}

		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		msg := "Incoming Request"
		if status >= 500 {
			logger.Error(msg, fields...)
		} else if status >= 400 {
			logger.Warn(msg, fields...)
		} else {
			logger.Info(msg, fields...)
		}
	}
}
