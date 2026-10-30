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

func NewRouter(deps Dependencies) *gin.Engine {
	engine := gin.New()
	engine.HandleMethodNotAllowed = false
	observability := deps.Middleware.Observability
	engine.Use(
		RequestIDMiddleware(deps.Middleware.IDs, deps.Middleware.RequestContext),
		TracingMiddleware(observability.Spans),
		AccessLogMiddleware(observability.Logger, observability.Clock),
		MetricsMiddleware(observability.Observer, observability.Clock),
		RecoveryMiddleware(deps.Middleware.Panics),
		TimeoutMiddleware(deps.Middleware.RequestTimeout),
	)
	engine.NoRoute(func(c *gin.Context) {
		presentProblem(c, problemKind{http.StatusNotFound, "not-found", "Not found"}, "route not found")
	})
	registerHealthRoutes(engine, deps.Controllers.Health)
	owner := ownerGroup(engine, deps.Access)
	registerAccountRoutes(owner, deps.Controllers.Account)
	registerTransferRoutes(owner, deps.Controllers.Transfer)
	registerAdminRoutes(adminGroup(engine, deps.Access), deps.Controllers.Admin)
	return engine
}

func ownerGroup(engine *gin.Engine, access Access) *gin.RouterGroup {
	return engine.Group("/v1", AuthMiddleware(access.Authenticator), RateLimitMiddleware(access.Limiter))
}

func adminGroup(engine *gin.Engine, access Access) *gin.RouterGroup {
	return engine.Group("/admin/v1", AdminAuthMiddleware(access.Admin))
}
