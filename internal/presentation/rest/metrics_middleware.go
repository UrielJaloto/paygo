package rest

import (
	"time"

	"github.com/gin-gonic/gin"
)

type RequestObserver interface {
	ObserveRequest(method, route string) func(status int, duration time.Duration)
}

func NewMetricsMiddleware(observer RequestObserver, clock Clock) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := clock.Now()
		finish := observer.ObserveRequest(c.Request.Method, readRoute(c))
		c.Next()
		finish(c.Writer.Status(), clock.Now().Sub(startedAt))
	}
}
