package rest

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

const unmatchedRoute = "unmatched"

type RequestLogger interface {
	LogRequest(ctx context.Context, method, route string) func(status int, duration time.Duration, err error)
}

func NewAccessLogMiddleware(logger RequestLogger, now func() time.Time) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		startedAt := now()
		finish := logger.LogRequest(ginContext.Request.Context(), ginContext.Request.Method, readRoute(ginContext))
		ginContext.Next()
		finish(ginContext.Writer.Status(), now().Sub(startedAt), findHandlerError(ginContext))
	}
}

func findHandlerError(ginContext *gin.Context) error {
	if last := ginContext.Errors.Last(); last != nil {
		return last.Err
	}
	return nil
}

func readRoute(ginContext *gin.Context) string {
	if route := ginContext.FullPath(); route != "" {
		return route
	}
	return unmatchedRoute
}
