package rest

import (
	"context"
	"errors"
	"net/http"

	transferservice "github.com/UrielJaloto/paygo/services/transfer"
	"github.com/gin-gonic/gin"
)

const (
	idempotencyKeyHeader      = "Idempotency-Key"
	idempotentReplayedHeader  = "Idempotent-Replayed"
	idempotentReplayedPresent = "true"
)

var errIdempotencyKeyRequired = errors.New("idempotency key required")

type TransferCreator interface {
	Transfer(ctx context.Context, input transferservice.TransferInput) (transferservice.TransferOutput, error)
}

type TransferReverser interface {
	Reverse(ctx context.Context, input transferservice.ReverseInput) (transferservice.TransferOutput, error)
}

type TransferReader interface {
	Get(ctx context.Context, query transferservice.TransferQuery) (transferservice.TransferOutput, error)
	List(ctx context.Context, input transferservice.ListInput) (transferservice.TransferPage, error)
}

type TransferController struct {
	creator   TransferCreator
	reverser  TransferReverser
	reader    TransferReader
	presenter TransferPresenter
}

type createTransferRequest struct {
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
}

func NewTransferController(creator TransferCreator, reverser TransferReverser, reader TransferReader, presenter TransferPresenter) *TransferController {
	return &TransferController{creator: creator, reverser: reverser, reader: reader, presenter: presenter}
}

func (t *TransferController) Create(c *gin.Context) {
	key, err := idempotencyKeyFrom(c)
	if err != nil {
		PresentError(c, err)
		return
	}
	var request createTransferRequest
	if err := decodeJSON(c, &request); err != nil {
		PresentError(c, err)
		return
	}
	output, err := t.creator.Transfer(c.Request.Context(), transferservice.TransferInput{
		OwnerID:        ownerFrom(c),
		IdempotencyKey: key,
		FromAccountID:  request.FromAccountID,
		ToAccountID:    request.ToAccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
	})
	t.presentMoneyMovement(c, output, err)
}

func (t *TransferController) Reverse(c *gin.Context) {
	key, err := idempotencyKeyFrom(c)
	if err != nil {
		PresentError(c, err)
		return
	}
	output, err := t.reverser.Reverse(c.Request.Context(), transferservice.ReverseInput{
		OwnerID:        ownerFrom(c),
		IdempotencyKey: key,
		TransferID:     c.Param("id"),
	})
	t.presentMoneyMovement(c, output, err)
}

func (t *TransferController) Get(c *gin.Context) {
	query := transferservice.TransferQuery{OwnerID: ownerFrom(c), TransferID: c.Param("id")}
	output, err := t.reader.Get(c.Request.Context(), query)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, t.presenter.transfer(output))
}

func (t *TransferController) List(c *gin.Context) {
	page, err := t.reader.List(c.Request.Context(), transferservice.ListInput{
		OwnerID:   ownerFrom(c),
		AccountID: c.Query("account_id"),
		Cursor:    c.Query("cursor"),
		Limit:     c.Query("limit"),
	})
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, t.presenter.transferPage(page))
}

func (t *TransferController) presentMoneyMovement(c *gin.Context, output transferservice.TransferOutput, err error) {
	if err != nil {
		PresentError(c, err)
		return
	}
	status := http.StatusCreated
	if output.IsReplay {
		status = http.StatusOK
		c.Header(idempotentReplayedHeader, idempotentReplayedPresent)
	}
	c.JSON(status, t.presenter.transfer(output))
}

func idempotencyKeyFrom(c *gin.Context) (string, error) {
	key := c.GetHeader(idempotencyKeyHeader)
	if key == "" {
		return "", errIdempotencyKeyRequired
	}
	return key, nil
}

func registerTransferRoutes(group *gin.RouterGroup, controller *TransferController) {
	group.POST("/transfers", controller.Create)
	group.GET("/transfers", controller.List)
	group.GET("/transfers/:id", controller.Get)
	group.POST("/transfers/:id/reversal", controller.Reverse)
}
