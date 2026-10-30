package rest

import (
	"context"
	"errors"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

var errHandlerPanicked = errors.New("handler panicked")

type PanicReporter interface {
	ReportPanic(ctx context.Context, recovered any, stack []byte)
}

func RecoveryMiddleware(reporter PanicReporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				reporter.ReportPanic(c.Request.Context(), recovered, debug.Stack())
				PresentError(c, errHandlerPanicked)
			}
		}()
		c.Next()
	}
}
