package asaas_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/infrastructure/provider/asaas"
)

func newWebhookProvider() *asaas.Provider {
	return asaas.New("http://asaas.test", apiKey, webhookToken, emptyCache(), &http.Client{})
}

func tokenHeader(token string) http.Header {
	header := http.Header{}
	header.Set("asaas-access-token", token)
	return header
}

func TestParseWebhook_Events_MapToKinds(t *testing.T) {
	tests := []struct {
		event    string
		resource string
		want     payment.EventKind
	}{
		{"PAYMENT_RECEIVED", "payment", payment.EventChargePaid},
		{"PAYMENT_CONFIRMED", "payment", payment.EventChargePaid},
		{"PAYMENT_OVERDUE", "payment", payment.EventChargeExpired},
		{"TRANSFER_DONE", "transfer", payment.EventPayoutDone},
		{"TRANSFER_FAILED", "transfer", payment.EventPayoutFailed},
		{"TRANSFER_CANCELLED", "transfer", payment.EventPayoutFailed},
		{"PAYMENT_CREATED", "payment", payment.EventIgnored},
		{"TRANSFER_PENDING", "transfer", payment.EventIgnored},
	}
	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			body := `{"id":"evt_1","event":"` + tt.event + `","` + tt.resource + `":{"id":"res_1"}}`
			got, err := newWebhookProvider().ParseWebhook(context.Background(), tokenHeader(webhookToken), []byte(body))
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			want := payment.ProviderEvent{Provider: "asaas", EventID: "evt_1", ProviderID: "res_1", Kind: tt.want}
			if got != want {
				t.Fatalf("event = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseWebhook_WrongToken_ReturnsUnauthorized(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{"wrong", tokenHeader("other")},
		{"missing", http.Header{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newWebhookProvider().ParseWebhook(context.Background(), tt.header, []byte(`{}`))
			if !errors.Is(err, payment.ErrWebhookUnauthorized) {
				t.Fatalf("err = %v, want ErrWebhookUnauthorized", err)
			}
		})
	}
}

func TestParseWebhook_MalformedJSON_ReturnsRejected(t *testing.T) {
	_, err := newWebhookProvider().ParseWebhook(context.Background(), tokenHeader(webhookToken), []byte(`{`))
	if !errors.Is(err, payment.ErrProviderRejected) {
		t.Fatalf("err = %v, want ErrProviderRejected", err)
	}
}
