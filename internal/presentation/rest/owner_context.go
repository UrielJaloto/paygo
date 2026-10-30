package rest

import "github.com/gin-gonic/gin"

const ownerContextKey = "paygo.owner"

func setOwner(c *gin.Context, ownerID string) {
	c.Set(ownerContextKey, ownerID)
}

func ownerFrom(c *gin.Context) string {
	return c.GetString(ownerContextKey)
}
