package rest

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SpanStarter interface {
	StartServerSpan(ctx context.Context, method, route string, header http.Header) (context.Context, func(status int))
}

func NewTracingMiddleware(spans SpanStarter) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		ctx, end := spans.StartServerSpan(ginContext.Request.Context(), ginContext.Request.Method, readRoute(ginContext), ginContext.Request.Header)
		ginContext.Request = ginContext.Request.WithContext(ctx)
		ginContext.Next()
		end(ginContext.Writer.Status())
	}
}
