package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// RateLimiter manages per-http rate limiters.
type RateLimiter struct {
	clients map[string]*rate.Limiter
	mu      sync.RWMutex
	rps     rate.Limit
	burst   int
	logger  *zap.Logger
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rps float64, burst int, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*rate.Limiter),
		rps:     rate.Limit(rps),
		burst:   burst,
		logger:  logger,
	}
}

// getLimiter returns a rate limiter for the given http IP.
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.clients[ip]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	// Create new limiter for this http
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists = rl.clients[ip]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(rl.rps, rl.burst)
	rl.clients[ip] = limiter

	return limiter
}

// Middleware returns the Gin middleware handler.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := rl.getLimiter(ip)

		if !limiter.Allow() {
			rl.logger.Warn("Rate limit exceeded",
				zap.String("ip", ip),
				zap.String("path", c.Request.URL.Path),
			)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
