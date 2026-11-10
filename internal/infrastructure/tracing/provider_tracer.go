package tracing

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/trace"

	domainpayment "github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/services/payment"
)

type paymentProvider interface {
	Name() string
	CreatePixCharge(ctx context.Context, request payment.ChargeRequest) (payment.ChargeResult, error)
	SendPixPayout(ctx context.Context, request payment.PayoutRequest) (payment.PayoutResult, error)
	FetchCharge(ctx context.Context, providerID string) (string, error)
	FetchPayout(ctx context.Context, providerID string) (string, error)
	ParseWebhook(ctx context.Context, header http.Header, body []byte) (domainpayment.ProviderEvent, error)
}

type ProviderTracer struct {
	next   paymentProvider
	tracer trace.Tracer
}

func NewProviderTracer(next paymentProvider, tracer trace.Tracer) *ProviderTracer {
	return &ProviderTracer{next: next, tracer: tracer}
}

func (t *ProviderTracer) Name() string {
	return t.next.Name()
}

func (t *ProviderTracer) CreatePixCharge(ctx context.Context, request payment.ChargeRequest) (payment.ChargeResult, error) {
	ctx, span := t.tracer.Start(ctx, "provider.CreatePixCharge")
	result, err := t.next.CreatePixCharge(ctx, request)
	endSpan(span, err)
	return result, err
}

func (t *ProviderTracer) SendPixPayout(ctx context.Context, request payment.PayoutRequest) (payment.PayoutResult, error) {
	ctx, span := t.tracer.Start(ctx, "provider.SendPixPayout")
	result, err := t.next.SendPixPayout(ctx, request)
	endSpan(span, err)
	return result, err
}

func (t *ProviderTracer) FetchCharge(ctx context.Context, providerID string) (string, error) {
	ctx, span := t.tracer.Start(ctx, "provider.FetchCharge")
	kind, err := t.next.FetchCharge(ctx, providerID)
	endSpan(span, err)
	return kind, err
}

func (t *ProviderTracer) FetchPayout(ctx context.Context, providerID string) (string, error) {
	ctx, span := t.tracer.Start(ctx, "provider.FetchPayout")
	kind, err := t.next.FetchPayout(ctx, providerID)
	endSpan(span, err)
	return kind, err
}

func (t *ProviderTracer) ParseWebhook(ctx context.Context, header http.Header, body []byte) (domainpayment.ProviderEvent, error) {
	ctx, span := t.tracer.Start(ctx, "provider.ParseWebhook")
	occurred, err := t.next.ParseWebhook(ctx, header, body)
	endSpan(span, err)
	return occurred, err
}
