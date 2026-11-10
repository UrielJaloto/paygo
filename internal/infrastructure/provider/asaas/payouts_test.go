package asaas_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/asaas"
	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
)

const (
	pixKeyErrorResponse = `{"errors": [{"code": "invalid_action", "description": "A chave Pix informada não foi encontrada"}]}`
	transferRequest     = `{"value": 10.50, "pixAddressKey": "ana@example.com", "pixAddressKeyType": "EMAIL", "operationType": "PIX", "externalReference": "payout-1"}`
	transferResponse    = `{"object": "transfer", "id": "777eb7c8-b1a2-4356-8fd8-a1b0644b5282", "status": "PENDING", "value": 10.50, "operationType": "PIX", "externalReference": "payout-1"}`
)

func payoutRequest() paymentservice.PayoutRequest {
	return paymentservice.PayoutRequest{
		ExternalReference: "payout-1",
		PixKey:            "ana@example.com",
		PixKeyType:        "email",
		AmountMinor:       1050,
	}
}

func TestSendPixPayout_ValidRequest_SendsPixTransfer(t *testing.T) {
	provider, stub := newStub(t, emptyCache(), reply{http.StatusOK, transferResponse})
	got, err := provider.SendPixPayout(context.Background(), payoutRequest())
	if err != nil {
		t.Fatalf("SendPixPayout: %v", err)
	}
	if got.ProviderID != "777eb7c8-b1a2-4356-8fd8-a1b0644b5282" {
		t.Fatalf("provider id = %q", got.ProviderID)
	}
	if stub.requests[0].method != http.MethodPost || stub.requests[0].path != "/transfers" {
		t.Fatalf("request = %s %s", stub.requests[0].method, stub.requests[0].path)
	}
	assertJSONBody(t, stub.requests[0].body, transferRequest)
}

func TestSendPixPayout_Failures_ReturnMappedError(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		want  error
	}{
		{"invalid pix key", reply{http.StatusBadRequest, pixKeyErrorResponse}, payment.ErrInvalidPixKey},
		{"other rejection", reply{http.StatusBadRequest, errorResponse}, payment.ErrProviderRejected},
		{"server error", reply{http.StatusInternalServerError, ""}, payment.ErrProviderUnavailable},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, _ := newStub(t, emptyCache(), testCase.reply)
			if _, err := provider.SendPixPayout(context.Background(), payoutRequest()); !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestFetchPayout_Statuses_MapToKinds(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"DONE", payment.EventPayoutDone},
		{"FAILED", payment.EventPayoutFailed},
		{"CANCELLED", payment.EventPayoutFailed},
		{"PENDING", payment.EventIgnored},
		{"BANK_PROCESSING", payment.EventIgnored},
	}
	for _, testCase := range tests {
		t.Run(testCase.status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(`{"id":"tra","status":"` + testCase.status + `"}`))
			}))
			defer server.Close()
			provider := asaas.NewProvider(server.URL, apiKey, webhookToken, emptyCache(), server.Client())
			got, err := provider.FetchPayout(context.Background(), "tra")
			if err != nil {
				t.Fatalf("FetchPayout: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("kind = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFetchPayout_ServerError_ReturnsUnavailable(t *testing.T) {
	provider, _ := newStub(t, emptyCache(), reply{http.StatusServiceUnavailable, ""})
	if _, err := provider.FetchPayout(context.Background(), "tra"); !errors.Is(err, payment.ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
}
