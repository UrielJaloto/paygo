package rest

import (
	"context"
	"net/http"

	"github.com/UrielJaloto/paygo/domain/apikey"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
	transferservice "github.com/UrielJaloto/paygo/services/transfer"
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
	issuer    KeyIssuer
	revoker   KeyRevoker
	funder    AccountFunder
	presenter AdminPresenter
	transfers TransferPresenter
}

type fundingRequest struct {
	AccountID string `json:"account_id"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
}

func NewAdminController(issuer KeyIssuer, revoker KeyRevoker, funder AccountFunder, presenter AdminPresenter, transfers TransferPresenter) *AdminController {
	return &AdminController{issuer: issuer, revoker: revoker, funder: funder, presenter: presenter, transfers: transfers}
}

func (a *AdminController) IssueKey(c *gin.Context) {
	issued, err := a.issuer.Issue(c.Request.Context(), apikeyservice.IssueInput{OwnerID: c.Param("owner_id")})
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, a.presenter.toIssuedKeyResponse(issued))
}

func (a *AdminController) RevokeKey(c *gin.Context) {
	if err := a.revoker.Revoke(c.Request.Context(), apikeyservice.RevokeInput{KeyID: c.Param("id")}); err != nil {
		PresentError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *AdminController) Deposit(c *gin.Context) {
	a.handleFunding(c, a.funder.Deposit)
}

func (a *AdminController) Withdraw(c *gin.Context) {
	a.handleFunding(c, a.funder.Withdraw)
}

func (a *AdminController) handleFunding(c *gin.Context, operation fundingOperation) {
	key, err := idempotencyKeyFrom(c)
	if err != nil {
		PresentError(c, err)
		return
	}
	var request fundingRequest
	if err := decodeJSON(c, &request); err != nil {
		PresentError(c, err)
		return
	}
	output, err := operation(c.Request.Context(), transferservice.FundingInput{
		OwnerID:        string(apikey.SystemOwnerID),
		IdempotencyKey: key,
		AccountID:      request.AccountID,
		Amount:         request.Amount,
		Currency:       request.Currency,
	})
	a.transfers.moneyMovement(c, output, err)
}

func registerAdminRoutes(group *gin.RouterGroup, controller *AdminController) {
	group.POST("/owners/:owner_id/api-keys", controller.IssueKey)
	group.DELETE("/api-keys/:id", controller.RevokeKey)
	group.POST("/deposits", controller.Deposit)
	group.POST("/withdrawals", controller.Withdraw)
}
