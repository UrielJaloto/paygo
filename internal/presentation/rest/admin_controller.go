package rest

import (
	"context"
	"net/http"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
	"github.com/gin-gonic/gin"
)

type KeyIssuer interface {
	Issue(ctx context.Context, input apikeyservice.IssueInput) (apikeyservice.IssuedKey, error)
}

type KeyRevoker interface {
	Revoke(ctx context.Context, input apikeyservice.RevokeInput) error
}

type AccountFunder interface {
	Deposit(ctx context.Context, input transferservice.FundingInput) (transferservice.TransferOutput, error)
	Withdraw(ctx context.Context, input transferservice.FundingInput) (transferservice.TransferOutput, error)
}

type fundingOperation func(ctx context.Context, input transferservice.FundingInput) (transferservice.TransferOutput, error)

type AdminController struct {
	issuer  KeyIssuer
	revoker KeyRevoker
	funder  AccountFunder
}

type fundingRequest struct {
	AccountID string `json:"account_id"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
}

func NewAdminController(issuer KeyIssuer, revoker KeyRevoker, funder AccountFunder) *AdminController {
	return &AdminController{issuer: issuer, revoker: revoker, funder: funder}
}

func (a *AdminController) IssueKey(ginContext *gin.Context) {
	issued, err := a.issuer.Issue(ginContext.Request.Context(), apikeyservice.IssueInput{OwnerID: ginContext.Param("owner_id")})
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusCreated, toIssuedKeyResponse(issued))
}

func (a *AdminController) RevokeKey(ginContext *gin.Context) {
	if err := a.revoker.Revoke(ginContext.Request.Context(), apikeyservice.RevokeInput{KeyID: ginContext.Param("id")}); err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.Status(http.StatusNoContent)
}

func (a *AdminController) Deposit(ginContext *gin.Context) {
	a.handleFunding(ginContext, a.funder.Deposit)
}

func (a *AdminController) Withdraw(ginContext *gin.Context) {
	a.handleFunding(ginContext, a.funder.Withdraw)
}

func (a *AdminController) handleFunding(ginContext *gin.Context, operation fundingOperation) {
	key, err := readIdempotencyKey(ginContext)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	var request fundingRequest
	if err := decodeJSON(ginContext, &request); err != nil {
		PresentError(ginContext, err)
		return
	}
	output, err := operation(ginContext.Request.Context(), transferservice.FundingInput{
		OwnerID:        string(apikey.SystemOwnerID),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
	})
	presentMoneyMovement(ginContext, output, err)
}

func registerAdminRoutes(group *gin.RouterGroup, controller *AdminController) {
	group.POST("/owners/:owner_id/api-keys", controller.IssueKey)
	group.DELETE("/api-keys/:id", controller.RevokeKey)
	group.POST("/deposits", controller.Deposit)
	group.POST("/withdrawals", controller.Withdraw)
}
