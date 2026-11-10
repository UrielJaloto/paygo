package rest

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

func NewTimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		ctx, cancel := context.WithTimeout(ginContext.Request.Context(), timeout)
		defer cancel()
		ginContext.Request = ginContext.Request.WithContext(ctx)
		ginContext.Next()
	}
}
