package tracing_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	domainpayment "github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/internal/services/payment"
)

var errBoom = errors.New("boom")

type methodCase[D any] struct {
	span string
	want any
	call func(ctx context.Context, decorator D) (any, error)
}

func runSuite[D any](t *testing.T, build func(err error, tracer trace.Tracer) D, cases []methodCase[D]) {
	t.Helper()
	for _, method := range cases {
		t.Run(method.span+"/success", func(t *testing.T) { assertSpanOnSuccess(t, build, method) })
		t.Run(method.span+"/error", func(t *testing.T) { assertSpanOnError(t, build, method) })
	}
}

func assertSpanOnSuccess[D any](t *testing.T, build func(err error, tracer trace.Tracer) D, method methodCase[D]) {
	t.Helper()
	exporter, tracer := newRecorder(t)
	got, err := method.call(context.Background(), build(nil, tracer))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(got, method.want) {
		t.Errorf("output = %+v, want %+v", got, method.want)
	}
	span := singleSpan(t, exporter)
	if span.Name != method.span || span.Status.Code == codes.Error || len(span.Events) != 0 {
		t.Errorf("span = %s, status %v, events %d", span.Name, span.Status.Code, len(span.Events))
	}
}

func assertSpanOnError[D any](t *testing.T, build func(err error, tracer trace.Tracer) D, method methodCase[D]) {
	t.Helper()
	exporter, tracer := newRecorder(t)
	if _, err := method.call(context.Background(), build(errBoom, tracer)); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want %v", err, errBoom)
	}
	span := singleSpan(t, exporter)
	if span.Status.Code != codes.Error || len(span.Events) != 1 || span.Events[0].Name != "exception" {
		t.Errorf("status = %v, events = %+v", span.Status.Code, span.Events)
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

func (s stubProvider) FetchCharge(context.Context, string) (domainpayment.EventKind, error) {
	return s.kind, s.err
}

func (s stubProvider) FetchPayout(context.Context, string) (domainpayment.EventKind, error) {
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
		{span: "provider.FetchCharge", want: kind, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.FetchCharge(ctx, "id")
		}},
		{span: "provider.FetchPayout", want: kind, call: func(ctx context.Context, d *tracing.ProviderTracer) (any, error) {
			return d.FetchPayout(ctx, "id")
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
