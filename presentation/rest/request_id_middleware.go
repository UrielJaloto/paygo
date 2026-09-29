package rest

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader    = "X-Request-Id"
	maximumRequestIDLength = 64
	requestIDAlphabet  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-"
)

type IDGenerator interface {
	NewID() string
}

type RequestContextualizer interface {
	AttachRequestID(ctx context.Context, id string) context.Context
}

func NewRequestIDMiddleware(ids IDGenerator, contextualizer RequestContextualizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !isValidRequestID(id) {
			id = ids.NewID()
		}
		c.Header(requestIDHeader, id)
		c.Request = c.Request.WithContext(contextualizer.AttachRequestID(c.Request.Context(), id))
		c.Next()
	}
}

func isValidRequestID(id string) bool {
	return id != "" && len(id) <= maximumRequestIDLength && strings.Trim(id, requestIDAlphabet) == ""
}
