package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Auth checks for a valid Bearer token in the Authorization header.
func Auth(validKeys []string) gin.HandlerFunc {
	keyMap := make(map[string]bool)
	for _, k := range validKeys {
		keyMap[k] = true
	}

	return func(c *gin.Context) {
		// If no keys are configured, skip auth (or fail closed depending on policy)
		if len(keyMap) == 0 {
			c.Next()
			return
		}

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

		if !keyMap[token] {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API Key"})
			return
		}

		c.Next()
	}
}
