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
	return func(c *gin.Context) {
		if !limiter.Allow(ownerFrom(c)) {
			c.Header("Retry-After", "1")
			PresentError(c, errRateLimited)
			return
		}
		c.Next()
	}
}
