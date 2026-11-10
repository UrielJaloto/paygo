package rest

import (
	"context"
	"strings"

	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
	"github.com/gin-gonic/gin"
)

const bearerPrefix = "Bearer "

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (apikeyservice.Principal, error)
}

func NewAuthMiddleware(authenticator Authenticator) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		token, found := readBearerToken(ginContext)
		if !found {
			PresentError(ginContext, apikeyservice.ErrUnauthenticated)
			return
		}
		principal, err := authenticator.Authenticate(ginContext.Request.Context(), token)
		if err != nil {
			PresentError(ginContext, err)
			return
		}
		setOwner(ginContext, principal.OwnerID)
		ginContext.Next()
	}
}

func readBearerToken(ginContext *gin.Context) (string, bool) {
	token, found := strings.CutPrefix(ginContext.GetHeader("Authorization"), bearerPrefix)
	return token, found && token != ""
}
