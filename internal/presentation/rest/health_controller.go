package rest

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ReadinessChecker interface {
	IsReady(ctx context.Context) bool
}

type HealthController struct {
	readiness ReadinessChecker
	metrics   http.Handler
}

type statusResponse struct {
	Status string `json:"status"`
}

func NewHealthController(readiness ReadinessChecker, metrics http.Handler) *HealthController {
	return &HealthController{readiness: readiness, metrics: metrics}
}

func (h *HealthController) Live(c *gin.Context) {
	c.JSON(http.StatusOK, statusResponse{Status: "ok"})
}

func (h *HealthController) Ready(c *gin.Context) {
	if h.readiness.IsReady(c.Request.Context()) {
		c.JSON(http.StatusOK, statusResponse{Status: "ready"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
}

func registerHealthRoutes(engine *gin.Engine, controller *HealthController) {
	engine.GET("/healthz", controller.Live)
	engine.GET("/readyz", controller.Ready)
	engine.GET("/metrics", gin.WrapH(controller.metrics))
}
