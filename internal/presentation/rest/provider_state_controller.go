package rest

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
)

type WebhookParser interface {
	ParseWebhook(ctx context.Context, header http.Header, body []byte) (payment.ProviderEvent, error)
}

type ProviderStateController struct {
	parser WebhookParser
}

func NewProviderStateController(parser WebhookParser) *ProviderStateController {
	return &ProviderStateController{parser: parser}
}

func (p *ProviderStateController) Record(ginContext *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(ginContext.Writer, ginContext.Request.Body, maximumProviderWebhookBytes))
	if err != nil {
		PresentError(ginContext, classifyDecodeError(err))
		return
	}
	if _, err := p.parser.ParseWebhook(ginContext.Request.Context(), ginContext.Request.Header, body); err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, statusResponse{Status: "ok"})
}

func registerProviderStateRoutes(engine *gin.Engine, controller *ProviderStateController) {
	engine.POST("/provider/v1/webhooks/:provider", controller.Record)
}
