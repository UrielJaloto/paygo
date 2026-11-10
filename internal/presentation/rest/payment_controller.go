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
	services  PaymentServices
	presenter PaymentPresenter
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

func NewPaymentController(services PaymentServices, presenter PaymentPresenter) *PaymentController {
	return &PaymentController{services: services, presenter: presenter}
}

func (p *PaymentController) CreateCharge(c *gin.Context) {
	key, err := readIdempotencyKey(c)
	if err != nil {
		PresentError(c, err)
		return
	}
	var request createChargeRequest
	if err := decodeJSON(c, &request); err != nil {
		PresentError(c, err)
		return
	}
	output, err := p.services.ChargeCreator.CreateCharge(c.Request.Context(), paymentservice.ChargeInput{
		OwnerID:        readOwner(c),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
		PayerName:      request.Payer.Name,
		PayerDocument:  request.Payer.Document,
	})
	p.presenter.presentCreatedCharge(c, output, err)
}

func (p *PaymentController) FindCharge(c *gin.Context) {
	query := paymentservice.ChargeQuery{OwnerID: readOwner(c), ChargeID: c.Param("id")}
	output, err := p.services.ChargeReader.FindCharge(c.Request.Context(), query)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, p.presenter.toChargeResponse(output))
}

func (p *PaymentController) CreatePayout(c *gin.Context) {
	key, err := readIdempotencyKey(c)
	if err != nil {
		PresentError(c, err)
		return
	}
	var request createPayoutRequest
	if err := decodeJSON(c, &request); err != nil {
		PresentError(c, err)
		return
	}
	output, err := p.services.PayoutCreator.CreatePayout(c.Request.Context(), paymentservice.PayoutInput{
		OwnerID:        readOwner(c),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
		PixKey:         request.PixKey,
		PixKeyType:     request.PixKeyType,
	})
	p.presenter.presentCreatedPayout(c, output, err)
}

func (p *PaymentController) FindPayout(c *gin.Context) {
	query := paymentservice.PayoutQuery{OwnerID: readOwner(c), PayoutID: c.Param("id")}
	output, err := p.services.PayoutReader.FindPayout(c.Request.Context(), query)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, p.presenter.toPayoutResponse(output))
}

func registerPaymentRoutes(group *gin.RouterGroup, controller *PaymentController) {
	group.POST("/charges", controller.CreateCharge)
	group.GET("/charges/:id", controller.FindCharge)
	group.POST("/payouts", controller.CreatePayout)
	group.GET("/payouts/:id", controller.FindPayout)
}
