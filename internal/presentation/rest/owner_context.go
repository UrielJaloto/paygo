package rest

import "github.com/gin-gonic/gin"

const ownerContextKey = "paygo.owner"

func setOwner(ginContext *gin.Context, ownerID string) {
	ginContext.Set(ownerContextKey, ownerID)
}

func readOwner(ginContext *gin.Context) string {
	return ginContext.GetString(ownerContextKey)
}
