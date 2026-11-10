package rest

import (
	"context"
	"errors"
	"net/http"

	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
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
	Find(ctx context.Context, query transferservice.TransferQuery) (transferservice.TransferOutput, error)
	List(ctx context.Context, input transferservice.ListInput) (transferservice.TransferPage, error)
}

type TransferController struct {
	creator  TransferCreator
	reverser TransferReverser
	reader   TransferReader
}

type createTransferRequest struct {
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
}

func NewTransferController(creator TransferCreator, reverser TransferReverser, reader TransferReader) *TransferController {
	return &TransferController{creator: creator, reverser: reverser, reader: reader}
}

func (t *TransferController) Create(ginContext *gin.Context) {
	key, err := readIdempotencyKey(ginContext)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	var request createTransferRequest
	if err := decodeJSON(ginContext, &request); err != nil {
		PresentError(ginContext, err)
		return
	}
	output, err := t.creator.Transfer(ginContext.Request.Context(), transferservice.TransferInput{
		OwnerID:        readOwner(ginContext),
		IdempotencyKey: key,
		FromAccountID:  request.FromAccountID,
		ToAccountID:    request.ToAccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
	})
	presentMoneyMovement(ginContext, output, err)
}

func (t *TransferController) Reverse(ginContext *gin.Context) {
	key, err := readIdempotencyKey(ginContext)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	output, err := t.reverser.Reverse(ginContext.Request.Context(), transferservice.ReverseInput{
		OwnerID:        readOwner(ginContext),
		IdempotencyKey: key,
		TransferID:     ginContext.Param("id"),
	})
	presentMoneyMovement(ginContext, output, err)
}

func (t *TransferController) Find(ginContext *gin.Context) {
	query := transferservice.TransferQuery{OwnerID: readOwner(ginContext), TransferID: ginContext.Param("id")}
	output, err := t.reader.Find(ginContext.Request.Context(), query)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toTransferResponse(output))
}

func (t *TransferController) List(ginContext *gin.Context) {
	page, err := t.reader.List(ginContext.Request.Context(), transferservice.ListInput{
		OwnerID:   readOwner(ginContext),
		AccountID: ginContext.Query("account_id"),
		Cursor:    ginContext.Query("cursor"),
		Limit:     ginContext.Query("limit"),
	})
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toTransferPageResponse(page))
}

func readIdempotencyKey(ginContext *gin.Context) (string, error) {
	key := ginContext.GetHeader(idempotencyKeyHeader)
	if key == "" {
		return "", errIdempotencyKeyRequired
	}
	return key, nil
}

func registerTransferRoutes(group *gin.RouterGroup, controller *TransferController) {
	group.POST("/transfers", controller.Create)
	group.GET("/transfers", controller.List)
	group.GET("/transfers/:id", controller.Find)
	group.POST("/transfers/:id/reversal", controller.Reverse)
}
