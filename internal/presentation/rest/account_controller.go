package rest

import (
	"context"
	"net/http"

	accountservice "github.com/UrielJaloto/paygo/internal/services/account"
	"github.com/gin-gonic/gin"
)

type AccountOpener interface {
	Open(ctx context.Context, input accountservice.OpenInput) (accountservice.AccountOutput, error)
}

type AccountReader interface {
	Find(ctx context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error)
	List(ctx context.Context, input accountservice.ListInput) (accountservice.AccountPage, error)
	CalculateBalance(ctx context.Context, query accountservice.AccountQuery) (accountservice.BalanceOutput, error)
	ListStatement(ctx context.Context, input accountservice.PageInput) (accountservice.StatementPage, error)
}

type AccountFreezer interface {
	Freeze(ctx context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error)
	Unfreeze(ctx context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error)
}

type AccountController struct {
	opener    AccountOpener
	reader    AccountReader
	freezer   AccountFreezer
	presenter AccountPresenter
}

type openAccountRequest struct {
	Currency string `json:"currency"`
}

func NewAccountController(opener AccountOpener, reader AccountReader, freezer AccountFreezer, presenter AccountPresenter) *AccountController {
	return &AccountController{opener: opener, reader: reader, freezer: freezer, presenter: presenter}
}

func (a *AccountController) Open(c *gin.Context) {
	var request openAccountRequest
	if err := decodeJSON(c, &request); err != nil {
		PresentError(c, err)
		return
	}
	output, err := a.opener.Open(c.Request.Context(), accountservice.OpenInput{OwnerID: readOwner(c), Currency: request.Currency})
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusCreated, a.presenter.toAccountResponse(output))
}

func (a *AccountController) Find(c *gin.Context) {
	output, err := a.reader.Find(c.Request.Context(), readAccountQuery(c))
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, a.presenter.toAccountResponse(output))
}

func (a *AccountController) List(c *gin.Context) {
	input := accountservice.ListInput{OwnerID: readOwner(c), Cursor: c.Query("cursor"), Limit: c.Query("limit")}
	page, err := a.reader.List(c.Request.Context(), input)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, a.presenter.toAccountPageResponse(page))
}

func (a *AccountController) CalculateBalance(c *gin.Context) {
	output, err := a.reader.CalculateBalance(c.Request.Context(), readAccountQuery(c))
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, a.presenter.toBalanceResponse(output))
}

func (a *AccountController) ListStatement(c *gin.Context) {
	input := accountservice.PageInput{
		OwnerID:   readOwner(c),
		AccountID: c.Param("id"),
		Cursor:    c.Query("cursor"),
		Limit:     c.Query("limit"),
	}
	page, err := a.reader.ListStatement(c.Request.Context(), input)
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, a.presenter.toStatementPageResponse(page))
}

func (a *AccountController) Freeze(c *gin.Context) {
	output, err := a.freezer.Freeze(c.Request.Context(), readAccountQuery(c))
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, a.presenter.toAccountResponse(output))
}

func (a *AccountController) Unfreeze(c *gin.Context) {
	output, err := a.freezer.Unfreeze(c.Request.Context(), readAccountQuery(c))
	if err != nil {
		PresentError(c, err)
		return
	}
	c.JSON(http.StatusOK, a.presenter.toAccountResponse(output))
}

func readAccountQuery(c *gin.Context) accountservice.AccountQuery {
	return accountservice.AccountQuery{OwnerID: readOwner(c), AccountID: c.Param("id")}
}

func registerAccountRoutes(group *gin.RouterGroup, controller *AccountController) {
	group.POST("/accounts", controller.Open)
	group.GET("/accounts", controller.List)
	group.GET("/accounts/:id", controller.Find)
	group.GET("/accounts/:id/balance", controller.CalculateBalance)
	group.GET("/accounts/:id/statement", controller.ListStatement)
	group.POST("/accounts/:id/freeze", controller.Freeze)
	group.POST("/accounts/:id/unfreeze", controller.Unfreeze)
}
