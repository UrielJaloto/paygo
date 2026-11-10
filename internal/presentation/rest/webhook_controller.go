package rest

import (
	"context"
	"net/http"

	webhookservice "github.com/UrielJaloto/paygo/internal/services/webhook"
	"github.com/gin-gonic/gin"
)

type EndpointRegistrar interface {
	Register(ctx context.Context, input webhookservice.RegisterInput) (webhookservice.RegisteredEndpoint, error)
}

type EndpointManager interface {
	ListEndpoints(ctx context.Context, ownerID string) ([]webhookservice.EndpointOutput, error)
	Disable(ctx context.Context, query webhookservice.EndpointQuery) error
}

type DeliveryManager interface {
	ListDeliveries(ctx context.Context, input webhookservice.DeliveryListInput) (webhookservice.DeliveryPage, error)
	Replay(ctx context.Context, query webhookservice.DeliveryQuery) (webhookservice.DeliveryOutput, error)
}

type WebhookController struct {
	registrar  EndpointRegistrar
	endpoints  EndpointManager
	deliveries DeliveryManager
}

type registerEndpointRequest struct {
	URL string `json:"url"`
}

func NewWebhookController(registrar EndpointRegistrar, endpoints EndpointManager, deliveries DeliveryManager) *WebhookController {
	return &WebhookController{registrar: registrar, endpoints: endpoints, deliveries: deliveries}
}

func (w *WebhookController) Register(ginContext *gin.Context) {
	var request registerEndpointRequest
	if err := decodeJSON(ginContext, &request); err != nil {
		PresentError(ginContext, err)
		return
	}
	input := webhookservice.RegisterInput{OwnerID: readOwner(ginContext), URL: request.URL}
	registered, err := w.registrar.Register(ginContext.Request.Context(), input)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusCreated, toRegisteredEndpointResponse(registered))
}

func (w *WebhookController) ListEndpoints(ginContext *gin.Context) {
	endpoints, err := w.endpoints.ListEndpoints(ginContext.Request.Context(), readOwner(ginContext))
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toEndpointListResponse(endpoints))
}

func (w *WebhookController) Disable(ginContext *gin.Context) {
	query := webhookservice.EndpointQuery{OwnerID: readOwner(ginContext), EndpointID: ginContext.Param("id")}
	if err := w.endpoints.Disable(ginContext.Request.Context(), query); err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.Status(http.StatusNoContent)
}

func (w *WebhookController) ListDeliveries(ginContext *gin.Context) {
	page, err := w.deliveries.ListDeliveries(ginContext.Request.Context(), webhookservice.DeliveryListInput{
		OwnerID: readOwner(ginContext),
		Status:  ginContext.Query("status"),
		Cursor:  ginContext.Query("cursor"),
		Limit:   ginContext.Query("limit"),
	})
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toDeliveryPageResponse(page))
}

func (w *WebhookController) Replay(ginContext *gin.Context) {
	query := webhookservice.DeliveryQuery{OwnerID: readOwner(ginContext), DeliveryID: ginContext.Param("id")}
	delivery, err := w.deliveries.Replay(ginContext.Request.Context(), query)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toDeliveryResponse(delivery))
}

func registerWebhookRoutes(group *gin.RouterGroup, controller *WebhookController) {
	group.POST("/webhook-endpoints", controller.Register)
	group.GET("/webhook-endpoints", controller.ListEndpoints)
	group.DELETE("/webhook-endpoints/:id", controller.Disable)
	group.GET("/webhook-deliveries", controller.ListDeliveries)
	group.POST("/webhook-deliveries/:id/replay", controller.Replay)
}
