package fake_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"net/http"
	"testing"

	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/infrastructure/provider/fake"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

const token = "0123456789abcdef0123456789abcdef"

type fixedIDs struct{}

func (fixedIDs) NewID() string {
	return "id"
}

func newProvider() *fake.Provider {
	return fake.NewMoney(token, fixedIDs{})
}

func validHeader() http.Header {
	return http.Header{"Paygo-Fake-Token": []string{token}}
}

func TestName_Always_ReturnsFake(t *testing.T) {
	if got := newProvider().Name(); got != "fake" {
		t.Fatalf("name = %q, want fake", got)
	}
}

func TestCreatePixCharge_PendingThenPaid_ReportsStatus(t *testing.T) {
	provider := newProvider()
	charge, err := provider.CreatePixCharge(context.Background(), paymentservice.ChargeRequest{})
	if err != nil {
		t.Fatalf("CreatePixCharge: %v", err)
	}
	if charge.ProviderID != "fake_id" || charge.PixCopyPaste != "00020126fakefake_id" {
		t.Fatalf("charge = %+v", charge)
	}
	assertCharge(t, provider, charge.ProviderID, payment.EventIgnored)
	provider.MarkPaid(charge.ProviderID)
	assertCharge(t, provider, charge.ProviderID, payment.EventChargePaid)
}

func TestCreatePixCharge_QRCode_IsPNG(t *testing.T) {
	charge, err := newProvider().CreatePixCharge(context.Background(), paymentservice.ChargeRequest{})
	if err != nil {
		t.Fatalf("CreatePixCharge: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(charge.PixQRBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("decode png: %v", err)
	}
}

func TestSendPixPayout_MarkPayoutState_ReportsStatus(t *testing.T) {
	tests := []struct {
		name string
		done bool
		want payment.EventKind
	}{
		{"done", true, payment.EventPayoutDone},
		{"failed", false, payment.EventPayoutFailed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newProvider()
			payout, err := provider.SendPixPayout(context.Background(), paymentservice.PayoutRequest{})
			if err != nil {
				t.Fatalf("SendPixPayout: %v", err)
			}
			assertPayout(t, provider, payout.ProviderID, payment.EventIgnored)
			provider.MarkPayout(payout.ProviderID, testCase.done)
			assertPayout(t, provider, payout.ProviderID, testCase.want)
		})
	}
}

func TestGetCharge_Unknown_ReturnsIgnored(t *testing.T) {
	assertCharge(t, newProvider(), "missing", payment.EventIgnored)
}

func TestParseWebhook_BadToken_ReturnsUnauthorized(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{"missing", http.Header{}},
		{"wrong", http.Header{"Paygo-Fake-Token": []string{"wrong"}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := newProvider().ParseWebhook(context.Background(), testCase.header, []byte(`{}`))
			if !errors.Is(err, payment.ErrWebhookUnauthorized) {
				t.Fatalf("err = %v, want ErrWebhookUnauthorized", err)
			}
		})
	}
}

func TestParseWebhook_MalformedJSON_ReturnsRejected(t *testing.T) {
	_, err := newProvider().ParseWebhook(context.Background(), validHeader(), []byte(`{`))
	if !errors.Is(err, payment.ErrProviderRejected) {
		t.Fatalf("err = %v, want ErrProviderRejected", err)
	}
}

func TestParseWebhook_Types_MapToKinds(t *testing.T) {
	tests := []struct {
		webhookType string
		want        payment.EventKind
	}{
		{"charge.paid", payment.EventChargePaid},
		{"payout.done", payment.EventPayoutDone},
		{"payout.failed", payment.EventPayoutFailed},
		{"other", payment.EventIgnored},
	}
	for _, testCase := range tests {
		t.Run(testCase.webhookType, func(t *testing.T) {
			body := []byte(`{"event_id":"e1","type":"` + testCase.webhookType + `","provider_id":"p1"}`)
			got, err := newProvider().ParseWebhook(context.Background(), validHeader(), body)
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			want := payment.ProviderEvent{Provider: "fake", EventID: "e1", ProviderID: "p1", Kind: testCase.want}
			if got != want {
				t.Fatalf("event = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseWebhook_SyncOnly_UpdatesStateWithoutEvent(t *testing.T) {
	provider := newProvider()
	body := []byte(`{"event_id":"e1","type":"charge.paid","provider_id":"p1","sync_only":true}`)
	got, err := provider.ParseWebhook(context.Background(), validHeader(), body)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	if got.Kind != payment.EventIgnored {
		t.Fatalf("kind = %q, want ignored", got.Kind)
	}
	assertCharge(t, provider, "p1", payment.EventChargePaid)
}

func TestParseWebhook_PayoutEvent_UpdatesState(t *testing.T) {
	provider := newProvider()
	body := []byte(`{"event_id":"e1","type":"payout.failed","provider_id":"p1"}`)
	if _, err := provider.ParseWebhook(context.Background(), validHeader(), body); err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	assertPayout(t, provider, "p1", payment.EventPayoutFailed)
}

func assertCharge(t *testing.T, provider *fake.Provider, id string, want payment.EventKind) {
	t.Helper()
	got, err := provider.GetCharge(context.Background(), id)
	if err != nil {
		t.Fatalf("GetCharge: %v", err)
	}
	if got != want {
		t.Fatalf("charge status = %q, want %q", got, want)
	}
}

func assertPayout(t *testing.T, provider *fake.Provider, id string, want payment.EventKind) {
	t.Helper()
	got, err := provider.GetPayout(context.Background(), id)
	if err != nil {
		t.Fatalf("GetPayout: %v", err)
	}
	if got != want {
		t.Fatalf("payout status = %q, want %q", got, want)
	}
}
