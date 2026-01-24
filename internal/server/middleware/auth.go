package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/store"
	"github.com/nulzo/model-router-api/pkg/api"
)

// Auth checks for a valid Bearer token in the Authorization header using the database.
func Auth(repo store.Repository, staticKeys []string) gin.HandlerFunc {
	staticMap := make(map[string]bool)
	for _, k := range staticKeys {
		staticMap[k] = true
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{Message: "Missing Authorization header"})
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{Message: "Invalid Authorization header format"})
			return
		}

		token := parts[1]

		// 1. Check static keys (config-based)
		if staticMap[token] {
			c.Next()
			return
		}

		// 2. Check DB keys
		hash := sha256.Sum256([]byte(token))
		hashedHex := hex.EncodeToString(hash[:])

		key, err := repo.APIKeys().GetByHash(c.Request.Context(), hashedHex)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, api.ErrorResponse{Message: "Invalid API Key"})
			return
		}

		// Inject key into context for downstream use (logging)
		ctx := context.WithValue(c.Request.Context(), store.ContextKeyAPIKey, key)
		c.Request = c.Request.WithContext(ctx)
		
		// Update last used timestamp (async)
		go func() {
			_ = repo.APIKeys().UpdateUsage(context.Background(), key.ID)
		}()

		c.Next()
	}
}
