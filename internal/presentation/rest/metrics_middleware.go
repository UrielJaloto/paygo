package rest

import (
	"time"

	"github.com/gin-gonic/gin"
)

type RequestObserver interface {
	ObserveRequest(method, route string) func(status int, duration time.Duration)
}

func NewMetricsMiddleware(observer RequestObserver, now func() time.Time) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		startedAt := now()
		finish := observer.ObserveRequest(ginContext.Request.Method, readRoute(ginContext))
		ginContext.Next()
		finish(ginContext.Writer.Status(), now().Sub(startedAt))
	}
}
