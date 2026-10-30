package rest

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader    = "X-Request-Id"
	maxRequestIDLength = 64
	requestIDAlphabet  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-"
)

type IDGenerator interface {
	NewID() string
}

type RequestContextualizer interface {
	ContextWithRequestID(ctx context.Context, id string) context.Context
}

func RequestIDMiddleware(ids IDGenerator, contextualizer RequestContextualizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !isValidRequestID(id) {
			id = ids.NewID()
		}
		c.Header(requestIDHeader, id)
		c.Request = c.Request.WithContext(contextualizer.ContextWithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

func isValidRequestID(id string) bool {
	return id != "" && len(id) <= maxRequestIDLength && strings.Trim(id, requestIDAlphabet) == ""
}
