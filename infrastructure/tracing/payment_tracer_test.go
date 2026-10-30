package tracing_test

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/trace"

	domainpayment "github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/infrastructure/tracing"
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

func TestPaymentServiceTracer_EveryMethod_RecordsSpan(t *testing.T) {
	charge := payment.ChargeOutput{ID: "charge-1"}
	payout := payment.PayoutOutput{ID: "payout-1"}
	synced := payment.SyncResult{Checked: 1}
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.PaymentServiceTracer {
		return tracing.NewPaymentServiceTracer(stubPayments{charge: charge, payout: payout, sync: synced, err: err}, tracer)
	}, []methodCase[*tracing.PaymentServiceTracer]{
		{span: "payment.CreateCharge", want: charge, call: func(ctx context.Context, d *tracing.PaymentServiceTracer) (any, error) {
			return d.CreateCharge(ctx, payment.ChargeInput{})
		}},
		{span: "payment.GetCharge", want: charge, call: func(ctx context.Context, d *tracing.PaymentServiceTracer) (any, error) {
			return d.GetCharge(ctx, payment.ChargeQuery{})
		}},
		{span: "payment.CreatePayout", want: payout, call: func(ctx context.Context, d *tracing.PaymentServiceTracer) (any, error) {
			return d.CreatePayout(ctx, payment.PayoutInput{})
		}},
		{span: "payment.GetPayout", want: payout, call: func(ctx context.Context, d *tracing.PaymentServiceTracer) (any, error) {
			return d.GetPayout(ctx, payment.PayoutQuery{})
		}},
		{span: "payment.HandleWebhook", want: nil, call: func(ctx context.Context, d *tracing.PaymentServiceTracer) (any, error) {
			return nil, d.HandleWebhook(ctx, payment.WebhookInput{})
		}},
		{span: "payment.Sync", want: synced, call: func(ctx context.Context, d *tracing.PaymentServiceTracer) (any, error) {
			return d.Sync(ctx, 1)
		}},
	})
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

func TestProviderTracer_EveryMethod_RecordsSpan(t *testing.T) {
	charge := payment.ChargeResult{ProviderID: "charge"}
	payout := payment.PayoutResult{ProviderID: "payout"}
	occurred := domainpayment.ProviderEvent{EventID: "event"}
	kind := domainpayment.EventChargePaid
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.ProviderTracer {
		return tracing.NewProviderTracer(stubProvider{charge: charge, payout: payout, kind: kind, occurred: occurred, err: err}, tracer)
	}, []methodCase[*tracing.ProviderTracer]{
		{span: "provider.CreatePixCharge", want: charge, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.CreatePixCharge(ctx, payment.ChargeRequest{})
		}},
		{span: "provider.SendPixPayout", want: payout, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.SendPixPayout(ctx, payment.PayoutRequest{})
		}},
		{span: "provider.GetCharge", want: kind, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.GetCharge(ctx, "id")
		}},
		{span: "provider.GetPayout", want: kind, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.GetPayout(ctx, "id")
		}},
		{span: "provider.ParseWebhook", want: occurred, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.ParseWebhook(ctx, nil, nil)
		}},
	})
}

func TestProviderTracer_Name_ReturnsProviderName(t *testing.T) {
	_, tracer := newRecorder(t)
	decorator := tracing.NewProviderTracer(stubProvider{}, tracer)
	if decorator.Name() != "fake" {
		t.Errorf("name = %q", decorator.Name())
	}
}
