package rest

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Middleware struct {
	IDs            IDGenerator
	RequestContext RequestContextualizer
	Panics         PanicReporter
	RequestTimeout time.Duration
}

type Controllers struct {
	Health *HealthController
}

type Dependencies struct {
	Middleware  Middleware
	Controllers Controllers
}

func NewRouter(deps Dependencies) *gin.Engine {
	engine := gin.New()
	engine.HandleMethodNotAllowed = false
	engine.Use(
		RequestIDMiddleware(deps.Middleware.IDs, deps.Middleware.RequestContext),
		RecoveryMiddleware(deps.Middleware.Panics),
		TimeoutMiddleware(deps.Middleware.RequestTimeout),
	)
	engine.NoRoute(func(c *gin.Context) {
		presentProblem(c, problemKind{http.StatusNotFound, "not-found", "Not found"}, "route not found")
	})
	registerHealthRoutes(engine, deps.Controllers.Health)
	return engine
}
