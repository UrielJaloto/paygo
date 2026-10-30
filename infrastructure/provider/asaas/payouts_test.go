package asaas_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/infrastructure/provider/asaas"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
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
	provider, stub := newStub(t, emptyCache(), reply{http.StatusOK, "transfer_response.json"})
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
	assertJSONFixture(t, stub.requests[0].body, "transfer_request.json")
}

func TestSendPixPayout_Failures_ReturnMappedError(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		want  error
	}{
		{"invalid pix key", reply{http.StatusBadRequest, "pix_key_error_response.json"}, payment.ErrInvalidPixKey},
		{"other rejection", reply{http.StatusBadRequest, "error_response.json"}, payment.ErrProviderRejected},
		{"server error", reply{http.StatusInternalServerError, ""}, payment.ErrProviderUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, _ := newStub(t, emptyCache(), tt.reply)
			if _, err := provider.SendPixPayout(context.Background(), payoutRequest()); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGetPayout_Statuses_MapToKinds(t *testing.T) {
	tests := []struct {
		status string
		want   payment.EventKind
	}{
		{"DONE", payment.EventPayoutDone},
		{"FAILED", payment.EventPayoutFailed},
		{"CANCELLED", payment.EventPayoutFailed},
		{"PENDING", payment.EventIgnored},
		{"BANK_PROCESSING", payment.EventIgnored},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"id":"tra","status":"` + tt.status + `"}`))
			}))
			defer server.Close()
			provider := asaas.New(server.URL, apiKey, webhookToken, emptyCache(), server.Client())
			got, err := provider.GetPayout(context.Background(), "tra")
			if err != nil {
				t.Fatalf("GetPayout: %v", err)
			}
			if got != tt.want {
				t.Fatalf("kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetPayout_ServerError_ReturnsUnavailable(t *testing.T) {
	provider, _ := newStub(t, emptyCache(), reply{http.StatusServiceUnavailable, ""})
	if _, err := provider.GetPayout(context.Background(), "tra"); !errors.Is(err, payment.ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
}
