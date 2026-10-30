package rest_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/presentation/rest"
	accountservice "github.com/UrielJaloto/paygo/internal/services/account"
	"github.com/gin-gonic/gin"
)

const accountJSON = `{"id":"acc-1","owner_id":"owner-1","currency":"BRL","kind":"customer","status":"active","created_at":"2026-01-02T03:04:05.678Z"}`

var accountCreatedAt = time.Date(2026, 1, 2, 0, 4, 5, 678_000_000, time.FixedZone("BRT", -3*60*60))

type stubAccounts struct {
	err       error
	open      accountservice.OpenInput
	query     accountservice.AccountQuery
	list      accountservice.ListInput
	statement accountservice.PageInput
	page      accountservice.AccountPage
	lines     accountservice.StatementPage
}

func accountOutput() accountservice.AccountOutput {
	return accountservice.AccountOutput{
		ID:        "acc-1",
		OwnerID:   ownerID,
		Currency:  "BRL",
		Kind:      "customer",
		Status:    "active",
		Version:   7,
		CreatedAt: accountCreatedAt,
	}
}

func (s *stubAccounts) Open(_ context.Context, input accountservice.OpenInput) (accountservice.AccountOutput, error) {
	s.open = input
	return accountOutput(), s.err
}

func (s *stubAccounts) Get(_ context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error) {
	s.query = query
	return accountOutput(), s.err
}

func (s *stubAccounts) List(_ context.Context, input accountservice.ListInput) (accountservice.AccountPage, error) {
	s.list = input
	return s.page, s.err
}

func (s *stubAccounts) CalculateBalance(_ context.Context, query accountservice.AccountQuery) (accountservice.BalanceOutput, error) {
	s.query = query
	return accountservice.BalanceOutput{AccountID: query.AccountID, AmountMinor: 300, Currency: "BRL"}, s.err
}

func (s *stubAccounts) ListStatement(_ context.Context, input accountservice.PageInput) (accountservice.StatementPage, error) {
	s.statement = input
	return s.lines, s.err
}

func (s *stubAccounts) Freeze(_ context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error) {
	s.query = query
	return accountOutput(), s.err
}

func (s *stubAccounts) Unfreeze(_ context.Context, query accountservice.AccountQuery) (accountservice.AccountOutput, error) {
	s.query = query
	return accountOutput(), s.err
}

func accountRouter(accounts *stubAccounts) *gin.Engine {
	controller := rest.NewAccountController(accounts, accounts, accounts, rest.AccountPresenter{})
	return apiRouter(rest.Controllers{Account: controller})
}

func TestAccountOpen_ValidBody_Returns201WithOwnerFromAuth(t *testing.T) {
	accounts := &stubAccounts{}
	recorder := call(accountRouter(accounts), http.MethodPost, "/v1/accounts", `{"currency":"BRL"}`)
	assertResponse(t, recorder, http.StatusCreated, accountJSON)
	if want := (accountservice.OpenInput{OwnerID: ownerID, Currency: "BRL"}); accounts.open != want {
		t.Fatalf("input = %+v", accounts.open)
	}
}

func TestAccountOpen_MissingCurrency_PassesEmptyAndReturns400(t *testing.T) {
	accounts := &stubAccounts{err: money.ErrUnsupportedCurrency}
	recorder := call(accountRouter(accounts), http.MethodPost, "/v1/accounts", `{}`)
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusBadRequest || body.Type != "/problems/invalid-request" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
	if accounts.open.Currency != "" {
		t.Fatalf("currency = %q", accounts.open.Currency)
	}
}

func TestAccountOpen_MalformedBody_Returns400WithoutCallingService(t *testing.T) {
	accounts := &stubAccounts{}
	recorder := call(accountRouter(accounts), http.MethodPost, "/v1/accounts", `{"currency":`)
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusBadRequest || body.Type != "/problems/malformed-request" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
	if accounts.open != (accountservice.OpenInput{}) {
		t.Fatalf("service called with %+v", accounts.open)
	}
}

func TestAccountFind_Existing_Returns200(t *testing.T) {
	accounts := &stubAccounts{}
	recorder := call(accountRouter(accounts), http.MethodGet, "/v1/accounts/acc-1", "")
	assertResponse(t, recorder, http.StatusOK, accountJSON)
	if want := (accountservice.AccountQuery{OwnerID: ownerID, AccountID: "acc-1"}); accounts.query != want {
		t.Fatalf("query = %+v", accounts.query)
	}
}

func TestAccountList_CursorAndLimit_ForwardedAndCursorReturned(t *testing.T) {
	accounts := &stubAccounts{page: accountservice.AccountPage{
		Items:      []accountservice.AccountOutput{accountOutput()},
		NextCursor: "next",
	}}
	recorder := call(accountRouter(accounts), http.MethodGet, "/v1/accounts?cursor=abc&limit=5", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[`+accountJSON+`],"next_cursor":"next"}`)
	if want := (accountservice.ListInput{OwnerID: ownerID, Cursor: "abc", Limit: "5"}); accounts.list != want {
		t.Fatalf("input = %+v", accounts.list)
	}
}

func TestAccountList_LastPage_OmitsCursorAndKeepsEmptyItems(t *testing.T) {
	recorder := call(accountRouter(&stubAccounts{}), http.MethodGet, "/v1/accounts", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[]}`)
}

func TestAccountFindBalance_Existing_ReturnsDecimalAmount(t *testing.T) {
	recorder := call(accountRouter(&stubAccounts{}), http.MethodGet, "/v1/accounts/acc-1/balance", "")
	assertResponse(t, recorder, http.StatusOK, `{"account_id":"acc-1","amount":"3.00","currency":"BRL"}`)
}

func TestAccountListStatement_Lines_ReturnsExactBody(t *testing.T) {
	accounts := &stubAccounts{lines: accountservice.StatementPage{
		Items: []accountservice.StatementLine{{
			EntryID:     "entry-1",
			TransferID:  "transfer-1",
			Direction:   "credit",
			AmountMinor: 1250,
			Currency:    "BRL",
			CreatedAt:   accountCreatedAt,
		}},
		NextCursor: "more",
	}}
	recorder := call(accountRouter(accounts), http.MethodGet, "/v1/accounts/acc-1/statement?cursor=c&limit=2", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[{"entry_id":"entry-1","transfer_id":"transfer-1","direction":"credit","amount":"12.50","currency":"BRL","created_at":"2026-01-02T03:04:05.678Z"}],"next_cursor":"more"}`)
	if want := (accountservice.PageInput{OwnerID: ownerID, AccountID: "acc-1", Cursor: "c", Limit: "2"}); accounts.statement != want {
		t.Fatalf("input = %+v", accounts.statement)
	}
}

func TestAccountFreeze_Success_Returns200(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"freeze", "/v1/accounts/acc-1/freeze"},
		{"unfreeze", "/v1/accounts/acc-1/unfreeze"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			accounts := &stubAccounts{}
			recorder := call(accountRouter(accounts), http.MethodPost, testCase.path, "")
			assertResponse(t, recorder, http.StatusOK, accountJSON)
			if want := (accountservice.AccountQuery{OwnerID: ownerID, AccountID: "acc-1"}); accounts.query != want {
				t.Fatalf("query = %+v", accounts.query)
			}
		})
	}
}

func TestAccountRoutes_ServiceError_ReturnsMappedProblem(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		err    error
		status int
	}{
		{"open", http.MethodPost, "/v1/accounts", money.ErrUnsupportedCurrency, http.StatusBadRequest},
		{"get", http.MethodGet, "/v1/accounts/acc-1", account.ErrAccountNotFound, http.StatusNotFound},
		{"list", http.MethodGet, "/v1/accounts", errBoom, http.StatusInternalServerError},
		{"balance", http.MethodGet, "/v1/accounts/acc-1/balance", account.ErrAccountNotFound, http.StatusNotFound},
		{"statement", http.MethodGet, "/v1/accounts/acc-1/statement", account.ErrAccountNotFound, http.StatusNotFound},
		{"freeze", http.MethodPost, "/v1/accounts/acc-1/freeze", account.ErrAccountAlreadyFrozen, http.StatusConflict},
		{"unfreeze", http.MethodPost, "/v1/accounts/acc-1/unfreeze", account.ErrAccountNotFrozen, http.StatusConflict},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := ""
			if testCase.name == "open" {
				body = `{"currency":"XXX"}`
			}
			recorder := call(accountRouter(&stubAccounts{err: testCase.err}), testCase.method, testCase.path, body)
			if problem := decodeProblem(t, recorder); recorder.Code != testCase.status || problem.Status != testCase.status {
				t.Fatalf("code = %d, problem = %+v", recorder.Code, problem)
			}
		})
	}
}

func TestAccountRoutes_NoCredentials_Return401(t *testing.T) {
	recorder := get(accountRouter(&stubAccounts{}), "/v1/accounts")
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusUnauthorized || body.Type != "/problems/unauthenticated" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
}
