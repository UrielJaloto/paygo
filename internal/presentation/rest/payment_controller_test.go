package rest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	"github.com/UrielJaloto/paygo/internal/presentation/rest"
	"github.com/UrielJaloto/paygo/internal/services/ledgercheck"
	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
	"github.com/gin-gonic/gin"
)

const (
	chargeBody = `{"account_id":"acc-1","amount":"10.50","currency":"BRL","payer":{"name":"Ana","document":"12345678909"}}`
	payoutBody = `{"account_id":"acc-1","amount":"10.50","currency":"BRL","pix_key":"a@b.co","pix_key_type":"email"}`
	chargeJSON = `{"id":"chg-1","account_id":"acc-1","status":"pending","amount":"10.50","currency":"BRL","pix_copy_paste":"pix","pix_qr_png_base64":"qr","expires_at":"2026-01-02T03:04:05.678Z","created_at":"2026-01-02T03:04:05.678Z"}`
	payoutJSON = `{"id":"pay-1","account_id":"acc-1","status":"processing","amount":"10.50","currency":"BRL","pix_key_type":"email","created_at":"2026-01-02T03:04:05.678Z"}`
)

var paymentAt = time.Date(2026, 1, 2, 3, 4, 5, 678_000_000, time.UTC)

type stubPayments struct {
	err         error
	replay      bool
	chargeInput paymentservice.ChargeInput
	chargeQuery paymentservice.ChargeQuery
	payoutInput paymentservice.PayoutInput
	payoutQuery paymentservice.PayoutQuery
	webhook     paymentservice.WebhookInput
	check       ledgercheck.CheckOutput
}

func (s *stubPayments) charge() paymentservice.ChargeOutput {
	return paymentservice.ChargeOutput{
		ID:           "chg-1",
		AccountID:    "acc-1",
		Status:       "pending",
		PixCopyPaste: "pix",
		PixQRBase64:  "qr",
		AmountMinor:  1050,
		Currency:     "BRL",
		ExpiresAt:    paymentAt,
		CreatedAt:    paymentAt,
		IsReplay:     s.replay,
	}
}

func (s *stubPayments) payout() paymentservice.PayoutOutput {
	return paymentservice.PayoutOutput{
		ID:          "pay-1",
		AccountID:   "acc-1",
		Status:      "processing",
		PixKeyType:  "email",
		AmountMinor: 1050,
		Currency:    "BRL",
		CreatedAt:   paymentAt,
		IsReplay:    s.replay,
	}
}

func (s *stubPayments) CreateCharge(_ context.Context, input paymentservice.ChargeInput) (paymentservice.ChargeOutput, error) {
	s.chargeInput = input
	return s.charge(), s.err
}

func (s *stubPayments) FindCharge(_ context.Context, query paymentservice.ChargeQuery) (paymentservice.ChargeOutput, error) {
	s.chargeQuery = query
	return s.charge(), s.err
}

func (s *stubPayments) CreatePayout(_ context.Context, input paymentservice.PayoutInput) (paymentservice.PayoutOutput, error) {
	s.payoutInput = input
	return s.payout(), s.err
}

func (s *stubPayments) FindPayout(_ context.Context, query paymentservice.PayoutQuery) (paymentservice.PayoutOutput, error) {
	s.payoutQuery = query
	return s.payout(), s.err
}

func (s *stubPayments) HandleWebhook(_ context.Context, input paymentservice.WebhookInput) error {
	s.webhook = input
	return s.err
}

func (s *stubPayments) Check(context.Context) (ledgercheck.CheckOutput, error) {
	return s.check, s.err
}

func paymentRouter(payments *stubPayments) *gin.Engine {
	services := rest.PaymentServices{ChargeCreator: payments, ChargeReader: payments, PayoutCreator: payments, PayoutReader: payments}
	return apiRouter(rest.Controllers{
		Payment:         rest.NewPaymentController(services, rest.PaymentPresenter{}),
		ProviderWebhook: rest.NewProviderWebhookController(payments),
		LedgerCheck:     rest.NewLedgerCheckController(payments),
	})
}

func providerWebhook(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/provider/v1/webhooks/asaas", strings.NewReader(body))
	request.Header.Set("Asaas-Access-Token", "token")
	return request
}

func TestPaymentCreateCharge_New_Returns201(t *testing.T) {
	payments := &stubPayments{}
	recorder := callWithKey(paymentRouter(payments), "/v1/charges", chargeBody)
	assertResponse(t, recorder, http.StatusCreated, chargeJSON)
	want := paymentservice.ChargeInput{
		OwnerID:        ownerID,
		IdempotencyKey: idempotencyKey,
		AccountID:      "acc-1",
		Amount:         "10.50",
		Currency:       "BRL",
		PayerName:      "Ana",
		PayerDocument:  "12345678909",
	}
	if payments.chargeInput != want {
		t.Fatalf("input = %+v", payments.chargeInput)
	}
}

func TestPaymentCreateCharge_Replay_Returns200WithHeader(t *testing.T) {
	recorder := callWithKey(paymentRouter(&stubPayments{replay: true}), "/v1/charges", chargeBody)
	assertResponse(t, recorder, http.StatusOK, chargeJSON)
	if got := recorder.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Fatalf("header = %q", got)
	}
}

func TestPaymentCreatePayout_New_Returns201(t *testing.T) {
	payments := &stubPayments{}
	recorder := callWithKey(paymentRouter(payments), "/v1/payouts", payoutBody)
	assertResponse(t, recorder, http.StatusCreated, payoutJSON)
	want := paymentservice.PayoutInput{
		OwnerID:        ownerID,
		IdempotencyKey: idempotencyKey,
		AccountID:      "acc-1",
		Amount:         "10.50",
		Currency:       "BRL",
		PixKey:         "a@b.co",
		PixKeyType:     "email",
	}
	if payments.payoutInput != want {
		t.Fatalf("input = %+v", payments.payoutInput)
	}
}

func TestPaymentCreatePayout_Replay_Returns200WithHeader(t *testing.T) {
	recorder := callWithKey(paymentRouter(&stubPayments{replay: true}), "/v1/payouts", payoutBody)
	assertResponse(t, recorder, http.StatusOK, payoutJSON)
	if got := recorder.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Fatalf("header = %q", got)
	}
}

func TestPaymentCreate_MissingIdempotencyKey_Returns400(t *testing.T) {
	for _, path := range []string{"/v1/charges", "/v1/payouts"} {
		t.Run(path, func(t *testing.T) {
			recorder := call(paymentRouter(&stubPayments{}), http.MethodPost, path, chargeBody)
			assertProblemType(t, recorder, http.StatusBadRequest, "idempotency-key-required")
		})
	}
}

func TestPaymentCreate_MalformedBody_Returns400(t *testing.T) {
	for _, path := range []string{"/v1/charges", "/v1/payouts"} {
		t.Run(path, func(t *testing.T) {
			recorder := callWithKey(paymentRouter(&stubPayments{}), path, `{"unknown":1}`)
			assertProblemType(t, recorder, http.StatusBadRequest, "malformed-request")
		})
	}
}

func TestPaymentCreate_ServiceError_ReturnsMappedProblem(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		body   string
		err    error
		status int
		slug   string
	}{
		{"invalid document", "/v1/charges", chargeBody, payment.ErrInvalidDocument, http.StatusBadRequest, "invalid-request"},
		{"usd", "/v1/charges", chargeBody, payment.ErrUnsupportedProvider, http.StatusBadRequest, "invalid-request"},
		{"unavailable", "/v1/charges", chargeBody, payment.ErrProviderUnavailable, http.StatusBadGateway, "provider-unavailable"},
		{"rejected", "/v1/charges", chargeBody, payment.ErrProviderRejected, http.StatusUnprocessableEntity, "provider-rejected"},
		{"insufficient", "/v1/payouts", payoutBody, transfer.ErrInsufficientFunds, http.StatusUnprocessableEntity, "insufficient-funds"},
		{"invalid pix key", "/v1/payouts", payoutBody, payment.ErrInvalidPixKey, http.StatusBadRequest, "invalid-request"},
		{"invalid amount", "/v1/payouts", payoutBody, money.ErrInvalidAmount, http.StatusBadRequest, "invalid-request"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := callWithKey(paymentRouter(&stubPayments{err: testCase.err}), testCase.path, testCase.body)
			assertProblemType(t, recorder, testCase.status, testCase.slug)
		})
	}
}

func TestPaymentFindCharge_Own_Returns200(t *testing.T) {
	payments := &stubPayments{}
	recorder := call(paymentRouter(payments), http.MethodGet, "/v1/charges/chg-1", "")
	assertResponse(t, recorder, http.StatusOK, chargeJSON)
	if want := (paymentservice.ChargeQuery{OwnerID: ownerID, ChargeID: "chg-1"}); payments.chargeQuery != want {
		t.Fatalf("query = %+v", payments.chargeQuery)
	}
}

func TestPaymentFindPayout_Own_Returns200(t *testing.T) {
	payments := &stubPayments{}
	recorder := call(paymentRouter(payments), http.MethodGet, "/v1/payouts/pay-1", "")
	assertResponse(t, recorder, http.StatusOK, payoutJSON)
	if want := (paymentservice.PayoutQuery{OwnerID: ownerID, PayoutID: "pay-1"}); payments.payoutQuery != want {
		t.Fatalf("query = %+v", payments.payoutQuery)
	}
}

func TestPaymentFind_OtherOwner_Returns404(t *testing.T) {
	cases := []struct {
		path string
		err  error
	}{
		{"/v1/charges/chg-1", payment.ErrChargeNotFound},
		{"/v1/payouts/pay-1", payment.ErrPayoutNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			recorder := call(paymentRouter(&stubPayments{err: testCase.err}), http.MethodGet, testCase.path, "")
			assertProblemType(t, recorder, http.StatusNotFound, "not-found")
		})
	}
}

func TestProviderWebhook_ValidToken_Returns200WithoutAPIKey(t *testing.T) {
	payments := &stubPayments{}
	recorder := send(paymentRouter(payments), providerWebhook(`{"event":"x"}`))
	assertResponse(t, recorder, http.StatusOK, `{"status":"ok"}`)
	if payments.webhook.Provider != "asaas" || string(payments.webhook.Body) != `{"event":"x"}` {
		t.Fatalf("input = %+v", payments.webhook)
	}
	if got := payments.webhook.Header.Get("Asaas-Access-Token"); got != "token" {
		t.Fatalf("header = %q", got)
	}
}

func TestProviderWebhook_BadToken_Returns401(t *testing.T) {
	recorder := send(paymentRouter(&stubPayments{err: payment.ErrWebhookUnauthorized}), providerWebhook("{}"))
	assertProblemType(t, recorder, http.StatusUnauthorized, "unauthenticated")
}

func TestProviderWebhook_BodyOverLimit_Returns413(t *testing.T) {
	payments := &stubPayments{}
	recorder := send(paymentRouter(payments), providerWebhook(strings.Repeat("x", 300<<10)))
	assertProblemType(t, recorder, http.StatusRequestEntityTooLarge, "payload-too-large")
	if payments.webhook.Provider != "" {
		t.Fatal("handler called")
	}
}

func TestProviderWebhook_BodyAtLimit_Returns200(t *testing.T) {
	recorder := send(paymentRouter(&stubPayments{}), providerWebhook(strings.Repeat("x", 256<<10)))
	assertResponse(t, recorder, http.StatusOK, `{"status":"ok"}`)
}

func TestLedgerCheck_Results_ReturnStatusAndBody(t *testing.T) {
	cases := []struct {
		name   string
		output ledgercheck.CheckOutput
		status int
		body   string
	}{
		{"ok", ledgercheck.CheckOutput{OK: true, Accounts: 3}, http.StatusOK, `{"ok":true,"accounts":3}`},
		{"mismatch", ledgercheck.CheckOutput{Mismatches: []string{"acc-1"}}, http.StatusConflict, `{"ok":false,"mismatches":["acc-1"]}`},
		{"unbalanced", ledgercheck.CheckOutput{UnbalancedCurrencies: []string{"BRL"}}, http.StatusConflict, `{"ok":false,"unbalanced_currencies":["BRL"]}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := send(paymentRouter(&stubPayments{check: testCase.output}), adminRequest(http.MethodGet, "/admin/v1/ledger/check", ""))
			assertResponse(t, recorder, testCase.status, testCase.body)
		})
	}
}

func TestLedgerCheck_ServiceFailure_Returns500(t *testing.T) {
	recorder := send(paymentRouter(&stubPayments{err: errBoom}), adminRequest(http.MethodGet, "/admin/v1/ledger/check", ""))
	assertProblemType(t, recorder, http.StatusInternalServerError, "internal")
}
