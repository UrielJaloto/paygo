package rest

import (
	"context"
	"strings"

	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
	"github.com/gin-gonic/gin"
)

const bearerPrefix = "Bearer "

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (apikeyservice.Principal, error)
}

func AuthMiddleware(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, found := bearerToken(c)
		if !found {
			PresentError(c, apikeyservice.ErrUnauthenticated)
			return
		}
		principal, err := authenticator.Authenticate(c.Request.Context(), token)
		if err != nil {
			PresentError(c, err)
			return
		}
		setOwner(c, principal.OwnerID)
		c.Next()
	}
}

func bearerToken(c *gin.Context) (string, bool) {
	token, found := strings.CutPrefix(c.GetHeader("Authorization"), bearerPrefix)
	return token, found && token != ""
}
