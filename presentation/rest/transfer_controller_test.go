package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/presentation/rest"
	transferservice "github.com/UrielJaloto/paygo/services/transfer"
	"github.com/gin-gonic/gin"
)

const (
	idempotencyKey = "key-1"
	transferJSON   = `{"id":"trf-1","kind":"transfer","status":"completed","from_account_id":"acc-1","to_account_id":"acc-2","amount":"10.50","currency":"BRL","created_at":"2026-01-02T03:04:05.678Z"}`
	reversalJSON   = `{"id":"trf-1","kind":"transfer","status":"completed","from_account_id":"acc-1","to_account_id":"acc-2","amount":"10.50","currency":"BRL","reversal_of":"trf-0","created_at":"2026-01-02T03:04:05.678Z"}`
	createBody     = `{"from_account_id":"acc-1","to_account_id":"acc-2","amount":"10.50","currency":"BRL"}`
)

type stubTransfers struct {
	err      error
	replay   bool
	reversal string
	calls    int
	create   transferservice.TransferInput
	reverse  transferservice.ReverseInput
	query    transferservice.TransferQuery
	list     transferservice.ListInput
	page     transferservice.TransferPage
}

func (s *stubTransfers) output() transferservice.TransferOutput {
	return transferservice.TransferOutput{
		ID:            "trf-1",
		Kind:          "transfer",
		Status:        "completed",
		FromAccountID: "acc-1",
		ToAccountID:   "acc-2",
		AmountMinor:   1050,
		Currency:      "BRL",
		ReversalOf:    s.reversal,
		CreatedAt:     time.Date(2026, 1, 2, 3, 4, 5, 678_000_000, time.UTC),
		IsReplay:      s.replay,
	}
}

func (s *stubTransfers) Transfer(_ context.Context, input transferservice.TransferInput) (transferservice.TransferOutput, error) {
	s.calls++
	s.create = input
	return s.output(), s.err
}

func (s *stubTransfers) Reverse(_ context.Context, input transferservice.ReverseInput) (transferservice.TransferOutput, error) {
	s.calls++
	s.reverse = input
	return s.output(), s.err
}

func (s *stubTransfers) Get(_ context.Context, query transferservice.TransferQuery) (transferservice.TransferOutput, error) {
	s.query = query
	return s.output(), s.err
}

func (s *stubTransfers) List(_ context.Context, input transferservice.ListInput) (transferservice.TransferPage, error) {
	s.list = input
	return s.page, s.err
}

func transferRouter(transfers *stubTransfers) *gin.Engine {
	controller := rest.NewTransferController(transfers, transfers, transfers, rest.TransferPresenter{})
	return apiRouter(rest.Controllers{Transfer: controller})
}

func callWithKey(router http.Handler, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+ownerToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	return send(router, request)
}

func assertProblemType(t *testing.T, recorder *httptest.ResponseRecorder, status int, slug string) {
	t.Helper()
	if body := decodeProblem(t, recorder); recorder.Code != status || body.Type != "/problems/"+slug {
		t.Fatalf("code = %d, body = %+v, want %d %s", recorder.Code, body, status, slug)
	}
}

func TestTransferCreate_NewTransfer_Returns201WithExactInput(t *testing.T) {
	transfers := &stubTransfers{}
	recorder := callWithKey(transferRouter(transfers), "/v1/transfers", createBody)
	assertResponse(t, recorder, http.StatusCreated, transferJSON)
	if got := recorder.Header().Get("Idempotent-Replayed"); got != "" {
		t.Fatalf("Idempotent-Replayed = %q", got)
	}
	want := transferservice.TransferInput{
		OwnerID:        ownerID,
		IdempotencyKey: idempotencyKey,
		FromAccountID:  "acc-1",
		ToAccountID:    "acc-2",
		Amount:         "10.50",
		Currency:       "BRL",
	}
	if transfers.create != want {
		t.Fatalf("input = %+v", transfers.create)
	}
}

func TestTransferCreate_Replay_Returns200WithHeader(t *testing.T) {
	recorder := callWithKey(transferRouter(&stubTransfers{replay: true}), "/v1/transfers", createBody)
	assertResponse(t, recorder, http.StatusOK, transferJSON)
	if got := recorder.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Fatalf("Idempotent-Replayed = %q", got)
	}
}

func TestTransferPosts_MissingKey_Return400WithoutCallingService(t *testing.T) {
	for _, path := range []string{"/v1/transfers", "/v1/transfers/trf-0/reversal"} {
		t.Run(path, func(t *testing.T) {
			transfers := &stubTransfers{}
			recorder := call(transferRouter(transfers), http.MethodPost, path, createBody)
			assertProblemType(t, recorder, http.StatusBadRequest, "idempotency-key-required")
			if transfers.calls != 0 {
				t.Fatalf("service called %d times", transfers.calls)
			}
		})
	}
}

func TestTransferCreate_AmountAsNumber_Returns400Malformed(t *testing.T) {
	transfers := &stubTransfers{}
	body := `{"from_account_id":"acc-1","to_account_id":"acc-2","amount":10.5,"currency":"BRL"}`
	recorder := callWithKey(transferRouter(transfers), "/v1/transfers", body)
	assertProblemType(t, recorder, http.StatusBadRequest, "malformed-request")
	if transfers.calls != 0 {
		t.Fatalf("service called %d times", transfers.calls)
	}
}

func TestTransferReverse_NewReversal_Returns201WithExactInput(t *testing.T) {
	transfers := &stubTransfers{reversal: "trf-0"}
	recorder := callWithKey(transferRouter(transfers), "/v1/transfers/trf-0/reversal", "")
	assertResponse(t, recorder, http.StatusCreated, reversalJSON)
	want := transferservice.ReverseInput{OwnerID: ownerID, IdempotencyKey: idempotencyKey, TransferID: "trf-0"}
	if transfers.reverse != want {
		t.Fatalf("input = %+v", transfers.reverse)
	}
}

func TestTransferReverse_Replay_Returns200WithHeader(t *testing.T) {
	transfers := &stubTransfers{reversal: "trf-0", replay: true}
	recorder := callWithKey(transferRouter(transfers), "/v1/transfers/trf-0/reversal", "")
	assertResponse(t, recorder, http.StatusOK, reversalJSON)
	if got := recorder.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Fatalf("Idempotent-Replayed = %q", got)
	}
}

func TestTransferGet_Existing_Returns200(t *testing.T) {
	transfers := &stubTransfers{}
	recorder := call(transferRouter(transfers), http.MethodGet, "/v1/transfers/trf-1", "")
	assertResponse(t, recorder, http.StatusOK, transferJSON)
	if want := (transferservice.TransferQuery{OwnerID: ownerID, TransferID: "trf-1"}); transfers.query != want {
		t.Fatalf("query = %+v", transfers.query)
	}
}

func TestTransferList_Filters_ForwardedAndCursorReturned(t *testing.T) {
	transfers := &stubTransfers{}
	transfers.page = transferservice.TransferPage{Items: []transferservice.TransferOutput{transfers.output()}, NextCursor: "next"}
	recorder := call(transferRouter(transfers), http.MethodGet, "/v1/transfers?account_id=acc-1&cursor=c&limit=5", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[`+transferJSON+`],"next_cursor":"next"}`)
	want := transferservice.ListInput{OwnerID: ownerID, AccountID: "acc-1", Cursor: "c", Limit: "5"}
	if transfers.list != want {
		t.Fatalf("input = %+v", transfers.list)
	}
}

func TestTransferList_LastPage_OmitsCursorAndKeepsEmptyItems(t *testing.T) {
	recorder := call(transferRouter(&stubTransfers{}), http.MethodGet, "/v1/transfers", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[]}`)
}

func TestTransferRoutes_ServiceError_ReturnsMappedProblem(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		err    error
		status int
		slug   string
	}{
		{"idempotency conflict", http.MethodPost, "/v1/transfers", transfer.ErrIdempotencyConflict, http.StatusConflict, "idempotency-conflict"},
		{"insufficient funds", http.MethodPost, "/v1/transfers", transfer.ErrInsufficientFunds, http.StatusUnprocessableEntity, "insufficient-funds"},
		{"frozen account", http.MethodPost, "/v1/transfers", account.ErrAccountFrozen, http.StatusConflict, "conflict"},
		{"already reversed", http.MethodPost, "/v1/transfers/trf-0/reversal", transfer.ErrTransferAlreadyReversed, http.StatusConflict, "conflict"},
		{"get not found", http.MethodGet, "/v1/transfers/trf-0", transfer.ErrTransferNotFound, http.StatusNotFound, "not-found"},
		{"list failure", http.MethodGet, "/v1/transfers", errBoom, http.StatusInternalServerError, "internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(createBody))
			request.Header.Set("Authorization", "Bearer "+ownerToken)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", idempotencyKey)
			assertProblemType(t, send(transferRouter(&stubTransfers{err: tc.err}), request), tc.status, tc.slug)
		})
	}
}
