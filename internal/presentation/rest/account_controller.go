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
	FindBalance(ctx context.Context, query accountservice.AccountQuery) (accountservice.BalanceOutput, error)
	ListStatement(ctx context.Context, input accountservice.PageInput) (accountservice.StatementPage, error)
}

type AccountFreezer interface {
	Freeze(ctx context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error)
	Unfreeze(ctx context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error)
}

type AccountController struct {
	opener  AccountOpener
	reader  AccountReader
	freezer AccountFreezer
}

type openAccountRequest struct {
	Currency string `json:"currency"`
}

func NewAccountController(opener AccountOpener, reader AccountReader, freezer AccountFreezer) *AccountController {
	return &AccountController{opener: opener, reader: reader, freezer: freezer}
}

func (a *AccountController) Open(ginContext *gin.Context) {
	var request openAccountRequest
	if err := decodeJSON(ginContext, &request); err != nil {
		PresentError(ginContext, err)
		return
	}
	output, err := a.opener.Open(ginContext.Request.Context(), accountservice.OpenInput{OwnerID: readOwner(ginContext), Currency: request.Currency})
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusCreated, toAccountResponse(output))
}

func (a *AccountController) Find(ginContext *gin.Context) {
	output, err := a.reader.Find(ginContext.Request.Context(), readAccountQuery(ginContext))
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toAccountResponse(output))
}

func (a *AccountController) List(ginContext *gin.Context) {
	input := accountservice.ListInput{OwnerID: readOwner(ginContext), Cursor: ginContext.Query("cursor"), Limit: ginContext.Query("limit")}
	page, err := a.reader.List(ginContext.Request.Context(), input)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toAccountPageResponse(page))
}

func (a *AccountController) FindBalance(ginContext *gin.Context) {
	output, err := a.reader.FindBalance(ginContext.Request.Context(), readAccountQuery(ginContext))
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toBalanceResponse(output))
}

func (a *AccountController) ListStatement(ginContext *gin.Context) {
	input := accountservice.PageInput{
		OwnerID:   readOwner(ginContext),
		AccountID: ginContext.Param("id"),
		Cursor:    ginContext.Query("cursor"),
		Limit:     ginContext.Query("limit"),
	}
	page, err := a.reader.ListStatement(ginContext.Request.Context(), input)
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toStatementPageResponse(page))
}

func (a *AccountController) Freeze(ginContext *gin.Context) {
	output, err := a.freezer.Freeze(ginContext.Request.Context(), readAccountQuery(ginContext))
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toAccountResponse(output))
}

func (a *AccountController) Unfreeze(ginContext *gin.Context) {
	output, err := a.freezer.Unfreeze(ginContext.Request.Context(), readAccountQuery(ginContext))
	if err != nil {
		PresentError(ginContext, err)
		return
	}
	ginContext.JSON(http.StatusOK, toAccountResponse(output))
}

func readAccountQuery(ginContext *gin.Context) accountservice.AccountQuery {
	return accountservice.AccountQuery{OwnerID: readOwner(ginContext), AccountID: ginContext.Param("id")}
}

func registerAccountRoutes(group *gin.RouterGroup, controller *AccountController) {
	group.POST("/accounts", controller.Open)
	group.GET("/accounts", controller.List)
	group.GET("/accounts/:id", controller.Find)
	group.GET("/accounts/:id/balance", controller.FindBalance)
	group.GET("/accounts/:id/statement", controller.ListStatement)
	group.POST("/accounts/:id/freeze", controller.Freeze)
	group.POST("/accounts/:id/unfreeze", controller.Unfreeze)
}
