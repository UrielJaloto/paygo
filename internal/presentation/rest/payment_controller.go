package rest

import (
	"context"
	"net/http"

	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
	"github.com/gin-gonic/gin"
)

type ChargeCreator interface {
	CreateCharge(ctx context.Context, input paymentservice.ChargeInput) (paymentservice.ChargeOutput, error)
}

type ChargeReader interface {
	FindCharge(ctx context.Context, query paymentservice.ChargeQuery) (paymentservice.ChargeOutput, error)
}

type PayoutCreator interface {
	CreatePayout(ctx context.Context, input paymentservice.PayoutInput) (paymentservice.PayoutOutput, error)
}

type PayoutReader interface {
	FindPayout(ctx context.Context, query paymentservice.PayoutQuery) (paymentservice.PayoutOutput, error)
}

type PaymentServices struct {
	ChargeCreator ChargeCreator
	ChargeReader  ChargeReader
	PayoutCreator PayoutCreator
	PayoutReader  PayoutReader
}

type PaymentController struct {
	services PaymentServices
}

type payerRequest struct {
	Name     string `json:"name"`
	Document string `json:"document"`
}

type createChargeRequest struct {
	AccountID string       `json:"account_id"`
	Amount    string       `json:"amount"`
	Currency  string       `json:"currency"`
	Payer     payerRequest `json:"payer"`
}

type createPayoutRequest struct {
	AccountID  string `json:"account_id"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	PixKey     string `json:"pix_key"`
	PixKeyType string `json:"pix_key_type"`
}

func NewPaymentController(services PaymentServices) *PaymentController {
	return &PaymentController{services: services}
}

func (p *PaymentController) CreateCharge(ginContext *gin.Context) {
	key, err := readIdempotencyKey(ginContext)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	var request createChargeRequest
	if err := decodeJSON(ginContext, &request); err != nil {
		PresentError(ginContext, err)
		return
	}
	output, err := p.services.ChargeCreator.CreateCharge(ginContext.Request.Context(), paymentservice.ChargeInput{
		OwnerID:        readOwner(ginContext),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
		PayerName:      request.Payer.Name,
		PayerDocument:  request.Payer.Document,
	})
	presentCreatedCharge(ginContext, output, err)
}

func (p *PaymentController) FindCharge(ginContext *gin.Context) {
	query := paymentservice.ChargeQuery{OwnerID: readOwner(ginContext), ChargeID: ginContext.Param("id")}
	output, err := p.services.ChargeReader.FindCharge(ginContext.Request.Context(), query)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toChargeResponse(output))
}

func (p *PaymentController) CreatePayout(ginContext *gin.Context) {
	key, err := readIdempotencyKey(ginContext)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	var request createPayoutRequest
	if err := decodeJSON(ginContext, &request); err != nil {
		PresentError(ginContext, err)
		return
	}
	output, err := p.services.PayoutCreator.CreatePayout(ginContext.Request.Context(), paymentservice.PayoutInput{
		OwnerID:        readOwner(ginContext),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
		PixKey:         request.PixKey,
		PixKeyType:     request.PixKeyType,
	})
	presentCreatedPayout(ginContext, output, err)
}

func (p *PaymentController) FindPayout(ginContext *gin.Context) {
	query := paymentservice.PayoutQuery{OwnerID: readOwner(ginContext), PayoutID: ginContext.Param("id")}
	output, err := p.services.PayoutReader.FindPayout(ginContext.Request.Context(), query)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toPayoutResponse(output))
}

func registerPaymentRoutes(group *gin.RouterGroup, controller *PaymentController) {
	group.POST("/charges", controller.CreateCharge)
	group.GET("/charges/:id", controller.FindCharge)
	group.POST("/payouts", controller.CreatePayout)
	group.GET("/payouts/:id", controller.FindPayout)
}
