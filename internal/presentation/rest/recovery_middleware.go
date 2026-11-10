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

func NewRecoveryMiddleware(reporter PanicReporter) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				reporter.ReportPanic(ginContext.Request.Context(), recovered, debug.Stack())
				PresentError(ginContext, errHandlerPanicked)
			}
		}()
		ginContext.Next()
	}
}
