package rest

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

const unmatchedRoute = "unmatched"

type Clock interface {
	Now() time.Time
}

type RequestLogger interface {
	LogRequest(ctx context.Context, method, route string) func(status int, duration time.Duration)
}

func NewAccessLogMiddleware(logger RequestLogger, clock Clock) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := clock.Now()
		finish := logger.LogRequest(c.Request.Context(), c.Request.Method, readRoute(c))
		c.Next()
		finish(c.Writer.Status(), clock.Now().Sub(startedAt))
	}
}

func readRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return unmatchedRoute
}
