package rest

import (
	"context"
	"net/http"

	paymentservice "github.com/UrielJaloto/paygo/services/payment"
	"github.com/gin-gonic/gin"
)

type ChargeCreator interface {
	CreateCharge(ctx context.Context, input paymentservice.ChargeInput) (paymentservice.ChargeOutput, error)
}

type ChargeReader interface {
	GetCharge(ctx context.Context, query paymentservice.ChargeQuery) (paymentservice.ChargeOutput, error)
}

type PayoutCreator interface {
	CreatePayout(ctx context.Context, input paymentservice.PayoutInput) (paymentservice.PayoutOutput, error)
}

type PayoutReader interface {
	GetPayout(ctx context.Context, query paymentservice.PayoutQuery) (paymentservice.PayoutOutput, error)
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
	key, err := idempotencyKeyFrom(c)
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
		OwnerID:        ownerFrom(c),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
		PayerName:      request.Payer.Name,
		PayerDocument:  request.Payer.Document,
	})
	p.presenter.createdCharge(c, output, err)
}

func (p *PaymentController) GetCharge(c *gin.Context) {
	query := paymentservice.ChargeQuery{OwnerID: ownerFrom(c), ChargeID: c.Param("id")}
	output, err := p.services.ChargeReader.GetCharge(c.Request.Context(), query)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, p.presenter.charge(output))
}

func (p *PaymentController) CreatePayout(c *gin.Context) {
	key, err := idempotencyKeyFrom(c)
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
		OwnerID:        ownerFrom(c),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
		PixKey:         request.PixKey,
		PixKeyType:     request.PixKeyType,
	})
	p.presenter.createdPayout(c, output, err)
}

func (p *PaymentController) GetPayout(c *gin.Context) {
	query := paymentservice.PayoutQuery{OwnerID: ownerFrom(c), PayoutID: c.Param("id")}
	output, err := p.services.PayoutReader.GetPayout(c.Request.Context(), query)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, p.presenter.payout(output))
}

func registerPaymentRoutes(group *gin.RouterGroup, controller *PaymentController) {
	group.POST("/charges", controller.CreateCharge)
	group.GET("/charges/:id", controller.GetCharge)
	group.POST("/payouts", controller.CreatePayout)
	group.GET("/payouts/:id", controller.GetPayout)
}
