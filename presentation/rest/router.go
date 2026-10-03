package rest

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Observability struct {
	Logger   RequestLogger
	Observer RequestObserver
	Spans    SpanStarter
	Clock    Clock
}

type Middleware struct {
	IDs            IDGenerator
	RequestContext RequestContextualizer
	Panics         PanicReporter
	RequestTimeout time.Duration
	Observability  Observability
}

type Access struct {
	Authenticator Authenticator
	Limiter       Limiter
	Admin         AdminChecker
}

type Controllers struct {
	Health   *HealthController
	Account  *AccountController
	Transfer *TransferController
	Admin    *AdminController
}

type Dependencies struct {
	Middleware  Middleware
	Access      Access
	Controllers Controllers
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	engine := gin.New()
	engine.HandleMethodNotAllowed = false
	observability := dependencies.Middleware.Observability
	engine.Use(
		NewRequestIDMiddleware(dependencies.Middleware.IDs, dependencies.Middleware.RequestContext),
		NewTracingMiddleware(observability.Spans),
		NewAccessLogMiddleware(observability.Logger, observability.Clock),
		NewMetricsMiddleware(observability.Observer, observability.Clock),
		NewRecoveryMiddleware(dependencies.Middleware.Panics),
		NewTimeoutMiddleware(dependencies.Middleware.RequestTimeout),
	)
	engine.NoRoute(func(c *gin.Context) {
		presentProblem(c, problemKind{http.StatusNotFound, "not-found", "Not found"}, "route not found")
	})
	registerHealthRoutes(engine, dependencies.Controllers.Health)
	owner := newOwnerGroup(engine, dependencies.Access)
	registerAccountRoutes(owner, dependencies.Controllers.Account)
	registerTransferRoutes(owner, dependencies.Controllers.Transfer)
	registerAdminRoutes(newAdminGroup(engine, dependencies.Access), dependencies.Controllers.Admin)
	return engine
}

func newOwnerGroup(engine *gin.Engine, access Access) *gin.RouterGroup {
	return engine.Group("/v1", NewAuthMiddleware(access.Authenticator), NewRateLimitMiddleware(access.Limiter))
}

func newAdminGroup(engine *gin.Engine, access Access) *gin.RouterGroup {
	return engine.Group("/admin/v1", NewAdminAuthMiddleware(access.Admin))
}
