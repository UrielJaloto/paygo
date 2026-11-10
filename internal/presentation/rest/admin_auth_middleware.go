package rest

import (
	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
	"github.com/gin-gonic/gin"
)

type AdminChecker interface {
	IsAdmin(token string) bool
}

func NewAdminAuthMiddleware(checker AdminChecker) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		token, found := readBearerToken(ginContext)
		if !found || !checker.IsAdmin(token) {
			PresentError(ginContext, apikeyservice.ErrUnauthenticated)
			return
		}
		ginContext.Next()
	}
}
