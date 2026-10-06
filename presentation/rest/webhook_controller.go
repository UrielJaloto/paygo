package rest

import (
	"context"
	"net/http"

	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
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
	presenter  WebhookPresenter
}

type registerEndpointRequest struct {
	URL string `json:"url"`
}

func NewWebhookController(registrar EndpointRegistrar, endpoints EndpointManager, deliveries DeliveryManager, presenter WebhookPresenter) *WebhookController {
	return &WebhookController{registrar: registrar, endpoints: endpoints, deliveries: deliveries, presenter: presenter}
}

func (w *WebhookController) Register(c *gin.Context) {
	var request registerEndpointRequest
	if err := decodeJSON(c, &request); err != nil {
		PresentError(c, err)
		return
	}
	input := webhookservice.RegisterInput{OwnerID: ownerFrom(c), URL: request.URL}
	registered, err := w.registrar.Register(c.Request.Context(), input)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, w.presenter.registered(registered))
}

func (w *WebhookController) ListEndpoints(c *gin.Context) {
	endpoints, err := w.endpoints.ListEndpoints(c.Request.Context(), ownerFrom(c))
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, w.presenter.endpointList(endpoints))
}

func (w *WebhookController) Disable(c *gin.Context) {
	query := webhookservice.EndpointQuery{OwnerID: ownerFrom(c), EndpointID: c.Param("id")}
	if err := w.endpoints.Disable(c.Request.Context(), query); err != nil {
		PresentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (w *WebhookController) ListDeliveries(c *gin.Context) {
	page, err := w.deliveries.ListDeliveries(c.Request.Context(), webhookservice.DeliveryListInput{
		OwnerID: ownerFrom(c),
		Status:  c.Query("status"),
		Cursor:  c.Query("cursor"),
		Limit:   c.Query("limit"),
	})
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, w.presenter.deliveryPage(page))
}

func (w *WebhookController) Replay(c *gin.Context) {
	query := webhookservice.DeliveryQuery{OwnerID: ownerFrom(c), DeliveryID: c.Param("id")}
	delivery, err := w.deliveries.Replay(c.Request.Context(), query)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, w.presenter.delivery(delivery))
}

func registerWebhookRoutes(group *gin.RouterGroup, controller *WebhookController) {
	group.POST("/webhook-endpoints", controller.Register)
	group.GET("/webhook-endpoints", controller.ListEndpoints)
	group.DELETE("/webhook-endpoints/:id", controller.Disable)
	group.GET("/webhook-deliveries", controller.ListDeliveries)
	group.POST("/webhook-deliveries/:id/replay", controller.Replay)
}
