package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/internal/store"
)

// Identity middleware extracts X-App-Name from headers
func Identity() gin.HandlerFunc {
	return func(c *gin.Context) {
		appName := c.GetHeader("X-App-Name")
		if appName != "" {
			ctx := context.WithValue(c.Request.Context(), store.ContextKeyAppName, appName)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
