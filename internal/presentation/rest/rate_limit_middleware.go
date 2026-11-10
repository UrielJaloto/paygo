package rest

import (
	"errors"

	"github.com/gin-gonic/gin"
)

var errRateLimited = errors.New("rate limit exceeded")

type Limiter interface {
	Allow(key string) bool
}

func NewRateLimitMiddleware(limiter Limiter) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		if !limiter.Allow(readOwner(ginContext)) {
			ginContext.Header("Retry-After", "1")
			PresentError(ginContext, errRateLimited)
			return
		}
		ginContext.Next()
	}
}
