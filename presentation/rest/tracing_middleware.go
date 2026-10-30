package rest

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SpanStarter interface {
	StartServerSpan(ctx context.Context, method, route string, header http.Header) (context.Context, func(status int))
}

func TracingMiddleware(spans SpanStarter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, end := spans.StartServerSpan(c.Request.Context(), c.Request.Method, routeOf(c), c.Request.Header)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		end(c.Writer.Status())
	}
}
