package rest

import (
	"context"
	"io"
	"net/http"

	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
	"github.com/gin-gonic/gin"
)

const maximumProviderWebhookBytes = 256 << 10

type WebhookHandler interface {
	HandleWebhook(ctx context.Context, input paymentservice.WebhookInput) error
}

type ProviderWebhookController struct {
	handler WebhookHandler
}

func NewProviderWebhookController(handler WebhookHandler) *ProviderWebhookController {
	return &ProviderWebhookController{handler: handler}
}

func (p *ProviderWebhookController) Handle(ginContext *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(ginContext.Writer, ginContext.Request.Body, maximumProviderWebhookBytes))
	if err != nil {
		PresentError(ginContext, classifyDecodeError(err))
		return
	}
	err = p.handler.HandleWebhook(ginContext.Request.Context(), paymentservice.WebhookInput{
		Provider: ginContext.Param("provider"),
		Header:   ginContext.Request.Header,
		Body:     body,
	})
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func registerProviderWebhookRoutes(engine *gin.Engine, controller *ProviderWebhookController) {
	engine.POST("/provider/v1/webhooks/:provider", controller.Handle)
}
