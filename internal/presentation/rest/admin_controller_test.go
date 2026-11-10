package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	"github.com/UrielJaloto/paygo/internal/presentation/rest"
	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
	"github.com/gin-gonic/gin"
)

const (
	fundingBody   = `{"account_id":"account-1","amount":"10.50","currency":"BRL"}`
	issuedKeyJSON = `{"id":"key-1","owner_id":"owner-1","token":"secret-token","created_at":"2026-01-02T03:04:05.678Z"}`
)

type stubAdminServices struct {
	stubTransfers
	issueErr error
	issue    apikeyservice.IssueInput
	revoke   apikeyservice.RevokeInput
	funding  transferservice.FundingInput
	funded   string
}

func (s *stubAdminServices) Issue(_ context.Context, input apikeyservice.IssueInput) (apikeyservice.IssuedKey, error) {
	s.issue = input
	return apikeyservice.IssuedKey{
		ID:        "key-1",
		OwnerID:   "owner-1",
		Token:     "secret-token",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 678_000_000, time.UTC),
	}, s.issueErr
}

func (s *stubAdminServices) Revoke(_ context.Context, input apikeyservice.RevokeInput) error {
	s.revoke = input
	return s.issueErr
}

func (s *stubAdminServices) Deposit(_ context.Context, input transferservice.FundingInput) (transferservice.TransferOutput, error) {
	s.calls++
	s.funded = "deposit"
	s.funding = input
	return s.output(), s.err
}

func (s *stubAdminServices) Withdraw(_ context.Context, input transferservice.FundingInput) (transferservice.TransferOutput, error) {
	s.calls++
	s.funded = "withdraw"
	s.funding = input
	return s.output(), s.err
}

func adminRouter(services *stubAdminServices) *gin.Engine {
	controller := rest.NewAdminController(services, services, services)
	return apiRouter(rest.Controllers{Admin: controller})
}

func adminRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func adminFunding(router http.Handler, path string) *httptest.ResponseRecorder {
	request := adminRequest(http.MethodPost, path, fundingBody)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	return send(router, request)
}

func TestAdminIssueKey_ValidOwner_Returns201WithToken(t *testing.T) {
	services := &stubAdminServices{}
	recorder := send(adminRouter(services), adminRequest(http.MethodPost, "/admin/v1/owners/owner-1/api-keys", ""))
	assertResponse(t, recorder, http.StatusCreated, issuedKeyJSON)
	if services.issue.OwnerID != "owner-1" {
		t.Fatalf("input = %+v", services.issue)
	}
}

func TestAdminIssueKey_InvalidOwner_Returns400(t *testing.T) {
	services := &stubAdminServices{issueErr: apikey.ErrInvalidOwner}
	recorder := send(adminRouter(services), adminRequest(http.MethodPost, "/admin/v1/owners/x/api-keys", ""))
	assertProblemType(t, recorder, http.StatusBadRequest, "invalid-request")
}

func TestAdminRevokeKey_Existing_Returns204(t *testing.T) {
	services := &stubAdminServices{}
	recorder := send(adminRouter(services), adminRequest(http.MethodDelete, "/admin/v1/api-keys/key-1", ""))
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("code = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if services.revoke.KeyID != "key-1" {
		t.Fatalf("input = %+v", services.revoke)
	}
}

func TestAdminRevokeKey_ServiceError_ReturnsMappedProblem(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		slug   string
	}{
		{"missing", apikey.ErrAPIKeyNotFound, http.StatusNotFound, "not-found"},
		{"twice", apikey.ErrAPIKeyAlreadyRevoked, http.StatusConflict, "conflict"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			router := adminRouter(&stubAdminServices{issueErr: testCase.err})
			recorder := send(router, adminRequest(http.MethodDelete, "/admin/v1/api-keys/key-1", ""))
			assertProblemType(t, recorder, testCase.status, testCase.slug)
		})
	}
}

func TestAdminFunding_NewMovement_Returns201WithSystemOwner(t *testing.T) {
	cases := []struct {
		path   string
		funded string
	}{
		{"/admin/v1/deposits", "deposit"},
		{"/admin/v1/withdrawals", "withdraw"},
	}
	for _, testCase := range cases {
		t.Run(testCase.funded, func(t *testing.T) {
			services := &stubAdminServices{}
			assertResponse(t, adminFunding(adminRouter(services), testCase.path), http.StatusCreated, transferJSON)
			want := transferservice.FundingInput{
				OwnerID:        string(apikey.SystemOwnerID),
				IdempotencyKey: idempotencyKey,
				AccountID:      "account-1",
				Amount:         "10.50",
				Currency:       "BRL",
			}
			if services.funding != want || services.funded != testCase.funded {
				t.Fatalf("funded = %s, input = %+v", services.funded, services.funding)
			}
		})
	}
}

func TestAdminDeposit_Replay_Returns200WithHeader(t *testing.T) {
	services := &stubAdminServices{}
	services.replay = true
	recorder := adminFunding(adminRouter(services), "/admin/v1/deposits")
	assertResponse(t, recorder, http.StatusOK, transferJSON)
	if got := recorder.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Fatalf("Idempotent-Replayed = %q", got)
	}
}

func TestAdminDeposit_MissingKey_Returns400WithoutCallingService(t *testing.T) {
	services := &stubAdminServices{}
	recorder := send(adminRouter(services), adminRequest(http.MethodPost, "/admin/v1/deposits", fundingBody))
	assertProblemType(t, recorder, http.StatusBadRequest, "idempotency-key-required")
	if services.calls != 0 {
		t.Fatalf("service called %d times", services.calls)
	}
}

func TestAdminDeposit_MalformedBody_Returns400WithoutCallingService(t *testing.T) {
	services := &stubAdminServices{}
	request := adminRequest(http.MethodPost, "/admin/v1/deposits", `{"amount":10.5}`)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	assertProblemType(t, send(adminRouter(services), request), http.StatusBadRequest, "malformed-request")
	if services.calls != 0 {
		t.Fatalf("service called %d times", services.calls)
	}
}

func TestAdminFunding_ServiceError_ReturnsMappedProblem(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		err    error
		status int
		slug   string
	}{
		{"withdraw insufficient", "/admin/v1/withdrawals", transfer.ErrInsufficientFunds, http.StatusUnprocessableEntity, "insufficient-funds"},
		{"deposit missing account", "/admin/v1/deposits", account.ErrAccountNotFound, http.StatusNotFound, "not-found"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			services := &stubAdminServices{}
			services.err = testCase.err
			assertProblemType(t, adminFunding(adminRouter(services), testCase.path), testCase.status, testCase.slug)
		})
	}
}

func TestAdminRoutes_OwnerKey_Returns401(t *testing.T) {
	services := &stubAdminServices{}
	recorder := call(adminRouter(services), http.MethodPost, "/admin/v1/deposits", fundingBody)
	assertProblemType(t, recorder, http.StatusUnauthorized, "unauthenticated")
	if services.calls != 0 {
		t.Fatalf("service called %d times", services.calls)
	}
}
