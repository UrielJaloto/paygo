package rest

import (
	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
	"github.com/gin-gonic/gin"
)

type AdminChecker interface {
	IsAdmin(token string) bool
}

func NewAdminAuthMiddleware(checker AdminChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, found := bearerToken(c)
		if !found || !checker.IsAdmin(token) {
			PresentError(c, apikeyservice.ErrUnauthenticated)
			return
		}
		c.Next()
	}
}
