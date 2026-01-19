package middleware

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nulzo/model-router-api/pkg/api"
)

// ErrorHandler is a custom error handling middleware that handles all errors returned by handlers
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// check if there is an error, if so, get the last error
		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err

			// first, we need to check if it's a custom error
			if problem, ok := err.(*api.Problem); ok {
				// if there is an internal log attached, log it
				if problem.Log != nil {
					log.Printf("Internal Error: %v", problem.Log)
				}

				// RFC 9457 dictates the json is at the root
				c.JSON(problem.Status, problem)
				c.Abort()
				return
			}

			// at this point it's an unknown error.
			// we just should to 500 for catch-all server error
			log.Printf("Unhandled Error: %v", err)

			// send the JSON response in a standard error shape
			c.JSON(http.StatusInternalServerError, api.NewError(
				http.StatusInternalServerError,
				"Internal Server Error",
				"An unexpected error occurred.",
			))

			// we want to prevent the other middleware from writing to the response
			c.Abort()
		}
	}
}