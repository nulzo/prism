package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// AuthMiddleware checks for a valid Bearer token in the Authorization header.
// In a real system, this would validate against a database of issued keys.
func AuthMiddleware(validKeys []string) gin.HandlerFunc {
	// Convert to map for O(1) lookups
	keyMap := make(map[string]bool)
	for _, k := range validKeys {
		keyMap[k] = true
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization header format"})
			return
		}

		token := parts[1]
		
		// For development/demo, if no keys configured, allow all (or fail open/closed depending on security stance)
		// Here we fail closed if keys are provided, else allow (mock mode)
		if len(keyMap) > 0 {
			if !keyMap[token] {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API Key"})
				return
			}
		}

		// Store user info in context if needed
		c.Set("user_id", "user_"+token[:min(len(token), 8)])
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
				"error": "Rate limit exceeded",
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

// StructuredLogger logs request details
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

		// In production, use zap or logrus
		// For now, standard log with structured-like format
		// Using gin.DefaultWriter which is stdout
		// Format: [TIME] STATUS | LATENCY | IP | METHOD | PATH | ERROR
		if status >= 500 {
			// Log error specifically
			gin.DefaultErrorWriter.Write([]byte(
				time.Now().Format(time.RFC3339) + " | " + 
				string(rune(status)) + " | " + 
				latency.String() + " | " + 
				clientIP + " | " + 
				method + " | " + 
				path + " | " + 
				c.Errors.String() + "\n",
			))
		}
	}
}
