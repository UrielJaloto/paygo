package metrics_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	domainpayment "github.com/UrielJaloto/paygo/domain/payment"
	domaintransfer "github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/infrastructure/metrics"
	"github.com/UrielJaloto/paygo/services/payment"
)

type stubPayments struct {
	charge payment.ChargeOutput
	payout payment.PayoutOutput
	sync   payment.SyncResult
	err    error
}

func (s stubPayments) CreateCharge(context.Context, payment.ChargeInput) (payment.ChargeOutput, error) {
	return s.charge, s.err
}

func (s stubPayments) GetCharge(context.Context, payment.ChargeQuery) (payment.ChargeOutput, error) {
	return s.charge, s.err
}

func (s stubPayments) CreatePayout(context.Context, payment.PayoutInput) (payment.PayoutOutput, error) {
	return s.payout, s.err
}

func (s stubPayments) GetPayout(context.Context, payment.PayoutQuery) (payment.PayoutOutput, error) {
	return s.payout, s.err
}

func (s stubPayments) HandleWebhook(context.Context, payment.WebhookInput) error {
	return s.err
}

func (s stubPayments) Sync(context.Context, int) (payment.SyncResult, error) {
	return s.sync, s.err
}

func TestPaymentServiceMetrics_Creations_CountByKindAndStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status string
	}{
		{name: "success uses the output status", status: "pending"},
		{name: "business error", err: fmt.Errorf("wrapped: %w", domaintransfer.ErrInsufficientFunds), status: "rejected"},
		{name: "provider rejection", err: domainpayment.ErrProviderRejected, status: "rejected"},
		{name: "unexpected error", err: errBoom, status: "error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			registry := metrics.NewRegistry()
			stub := stubPayments{err: tc.err}
			if tc.err == nil {
				stub.charge.Status = "pending"
				stub.payout.Status = "pending"
			}
			decorator := metrics.NewPaymentServiceMetrics(stub, registry)
			charge, chargeErr := decorator.CreateCharge(context.Background(), payment.ChargeInput{})
			payout, payoutErr := decorator.CreatePayout(context.Background(), payment.PayoutInput{})
			if !errors.Is(chargeErr, tc.err) || !errors.Is(payoutErr, tc.err) {
				t.Fatalf("errors = %v, %v, want %v", chargeErr, payoutErr, tc.err)
			}
			if charge != stub.charge || payout != stub.payout {
				t.Errorf("outputs = %+v, %+v", charge, payout)
			}
			assertSeries(t, scrape(t, registry),
				fmt.Sprintf(`paygo_payments_total{kind="charge",status=%q} 1`, tc.status),
				fmt.Sprintf(`paygo_payments_total{kind="payout",status=%q} 1`, tc.status),
			)
		})
	}
}

func TestPaymentServiceMetrics_OtherMethods_PassThroughWithoutCounting(t *testing.T) {
	registry := metrics.NewRegistry()
	stub := stubPayments{
		charge: payment.ChargeOutput{ID: "charge-1"},
		payout: payment.PayoutOutput{ID: "payout-1"},
		sync:   payment.SyncResult{Checked: 1},
		err:    errBoom,
	}
	decorator := metrics.NewPaymentServiceMetrics(stub, registry)
	ctx := context.Background()
	charge, chargeErr := decorator.GetCharge(ctx, payment.ChargeQuery{})
	payout, payoutErr := decorator.GetPayout(ctx, payment.PayoutQuery{})
	synced, syncErr := decorator.Sync(ctx, 1)
	hookErr := decorator.HandleWebhook(ctx, payment.WebhookInput{})
	for _, err := range []error{chargeErr, payoutErr, syncErr, hookErr} {
		if !errors.Is(err, errBoom) {
			t.Errorf("err = %v, want %v", err, errBoom)
		}
	}
	if charge != stub.charge || payout != stub.payout || synced != stub.sync {
		t.Errorf("outputs = %+v, %+v, %+v", charge, payout, synced)
	}
	if strings.Contains(scrape(t, registry), "paygo_payments_total{") {
		t.Error("queries must not count as payments")
	}
}

type stubProvider struct {
	charge   payment.ChargeResult
	payout   payment.PayoutResult
	kind     domainpayment.EventKind
	occurred domainpayment.ProviderEvent
	err      error
}

func (s stubProvider) Name() string {
	return "fake"
}

func (s stubProvider) CreatePixCharge(context.Context, payment.ChargeRequest) (payment.ChargeResult, error) {
	return s.charge, s.err
}

func (s stubProvider) SendPixPayout(context.Context, payment.PayoutRequest) (payment.PayoutResult, error) {
	return s.payout, s.err
}

func (s stubProvider) GetCharge(context.Context, string) (domainpayment.EventKind, error) {
	return s.kind, s.err
}

func (s stubProvider) GetPayout(context.Context, string) (domainpayment.EventKind, error) {
	return s.kind, s.err
}

func (s stubProvider) ParseWebhook(context.Context, http.Header, []byte) (domainpayment.ProviderEvent, error) {
	return s.occurred, s.err
}

func TestProviderMetrics_EveryOperation_CountsResultAndStaysTransparent(t *testing.T) {
	results := []struct {
		name   string
		err    error
		result string
	}{
		{name: "ok", result: "ok"},
		{name: "rejected", err: fmt.Errorf("wrapped: %w", domainpayment.ErrProviderRejected), result: "rejected"},
		{name: "unauthorized webhook", err: domainpayment.ErrWebhookUnauthorized, result: "rejected"},
		{name: "unavailable", err: domainpayment.ErrProviderUnavailable, result: "unavailable"},
	}
	for _, tc := range results {
		t.Run(tc.name, func(t *testing.T) {
			assertProviderCalls(t, tc.err, tc.result)
		})
	}
}

func assertProviderCalls(t *testing.T, want error, result string) {
	t.Helper()
	stub := stubProvider{
		charge:   payment.ChargeResult{ProviderID: "charge"},
		payout:   payment.PayoutResult{ProviderID: "payout"},
		kind:     domainpayment.EventChargePaid,
		occurred: domainpayment.ProviderEvent{EventID: "event"},
		err:      want,
	}
	registry := metrics.NewRegistry()
	decorator := metrics.NewProviderMetrics(stub, registry)
	ctx := context.Background()
	calls := []struct {
		operation string
		want      any
		call      func() (any, error)
	}{
		{"create_charge", stub.charge, func() (any, error) { return decorator.CreatePixCharge(ctx, payment.ChargeRequest{}) }},
		{"send_payout", stub.payout, func() (any, error) { return decorator.SendPixPayout(ctx, payment.PayoutRequest{}) }},
		{"get_charge", stub.kind, func() (any, error) { return decorator.GetCharge(ctx, "id") }},
		{"get_payout", stub.kind, func() (any, error) { return decorator.GetPayout(ctx, "id") }},
		{"parse_webhook", stub.occurred, func() (any, error) { return decorator.ParseWebhook(ctx, nil, nil) }},
	}
	for _, call := range calls {
		got, err := call.call()
		if !errors.Is(err, want) || !reflect.DeepEqual(got, call.want) {
			t.Errorf("%s = %+v, %v", call.operation, got, err)
		}
		assertSeries(t, scrape(t, registry), fmt.Sprintf(`paygo_provider_requests_total{operation=%q,provider="fake",result=%q} 1`, call.operation, result))
	}
	if decorator.Name() != "fake" {
		t.Errorf("name = %q", decorator.Name())
	}
}
