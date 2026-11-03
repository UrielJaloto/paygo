package rest

import (
	"context"
	"io"
	"net/http"

	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
	"github.com/gin-gonic/gin"
)

const maxProviderWebhookBytes = 256 << 10

type WebhookHandler interface {
	HandleWebhook(ctx context.Context, input paymentservice.WebhookInput) error
}

type ProviderWebhookController struct {
	handler WebhookHandler
}

func NewProviderWebhookController(handler WebhookHandler) *ProviderWebhookController {
	return &ProviderWebhookController{handler: handler}
}

func (p *ProviderWebhookController) Handle(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxProviderWebhookBytes))
	if err != nil {
		PresentError(c, decodeFailure(err))
		return
	}
	err = p.handler.HandleWebhook(c.Request.Context(), paymentservice.WebhookInput{
		Provider: c.Param("provider"),
		Header:   c.Request.Header,
		Body:     body,
	})
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func registerProviderWebhookRoutes(engine *gin.Engine, controller *ProviderWebhookController) {
	engine.POST("/provider/v1/webhooks/:provider", controller.Handle)
}
