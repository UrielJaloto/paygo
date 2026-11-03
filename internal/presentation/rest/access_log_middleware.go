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
	LogRequest(ctx context.Context, method, route string) func(status int, duration time.Duration, err error)
}

func AccessLogMiddleware(logger RequestLogger, clock Clock) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := clock.Now()
		finish := logger.LogRequest(c.Request.Context(), c.Request.Method, routeOf(c))
		c.Next()
		finish(c.Writer.Status(), clock.Now().Sub(startedAt), handlerError(c))
	}
}

func handlerError(c *gin.Context) error {
	if last := c.Errors.Last(); last != nil {
		return last.Err
	}
	return nil
}

func routeOf(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return unmatchedRoute
}
