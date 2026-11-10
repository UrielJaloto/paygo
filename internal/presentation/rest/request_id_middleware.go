package rest

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader        = "X-Request-Id"
	maximumRequestIDLength = 64
	requestIDAlphabet      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-"
)

type IDGenerator interface {
	NewID() string
}

type RequestContextualizer interface {
	AttachRequestID(ctx context.Context, id string) context.Context
}

func NewRequestIDMiddleware(ids IDGenerator, contextualizer RequestContextualizer) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		id := ginContext.GetHeader(requestIDHeader)
		if !isValidRequestID(id) {
			id = ids.NewID()
		}
		ginContext.Header(requestIDHeader, id)
		ginContext.Request = ginContext.Request.WithContext(contextualizer.AttachRequestID(ginContext.Request.Context(), id))
		ginContext.Next()
	}
}

func isValidRequestID(id string) bool {
	return id != "" && len(id) <= maximumRequestIDLength && strings.Trim(id, requestIDAlphabet) == ""
}
