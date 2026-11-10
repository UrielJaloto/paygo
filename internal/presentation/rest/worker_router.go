package rest

import "github.com/gin-gonic/gin"

type WorkerControllers struct {
	Health        *HealthController
	ProviderState *ProviderStateController
}

func NewWorkerRouter(panics PanicReporter, controllers WorkerControllers) *gin.Engine {
	engine := gin.New()
	engine.HandleMethodNotAllowed = false
	engine.Use(NewRecoveryMiddleware(panics))
	engine.NoRoute(presentRouteNotFound)
	registerHealthRoutes(engine, controllers.Health)
	registerProviderStateRoutes(engine, controllers.ProviderState)
	return engine
}
