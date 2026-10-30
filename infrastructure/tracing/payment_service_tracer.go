package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/services/payment"
)

type paymentService interface {
	CreateCharge(ctx context.Context, input payment.ChargeInput) (payment.ChargeOutput, error)
	GetCharge(ctx context.Context, query payment.ChargeQuery) (payment.ChargeOutput, error)
	CreatePayout(ctx context.Context, input payment.PayoutInput) (payment.PayoutOutput, error)
	GetPayout(ctx context.Context, query payment.PayoutQuery) (payment.PayoutOutput, error)
	HandleWebhook(ctx context.Context, input payment.WebhookInput) error
	Sync(ctx context.Context, limit int) (payment.SyncResult, error)
}

type PaymentServiceTracer struct {
	next   paymentService
	tracer trace.Tracer
}

func NewPaymentServiceTracer(next paymentService, tracer trace.Tracer) *PaymentServiceTracer {
	return &PaymentServiceTracer{next: next, tracer: tracer}
}

func (t *PaymentServiceTracer) CreateCharge(ctx context.Context, input payment.ChargeInput) (payment.ChargeOutput, error) {
	ctx, span := t.tracer.Start(ctx, "payment.CreateCharge")
	output, err := t.next.CreateCharge(ctx, input)
	finish(span, err)
	return output, err
}

func (t *PaymentServiceTracer) GetCharge(ctx context.Context, query payment.ChargeQuery) (payment.ChargeOutput, error) {
	ctx, span := t.tracer.Start(ctx, "payment.GetCharge")
	output, err := t.next.GetCharge(ctx, query)
	finish(span, err)
	return output, err
}

func (t *PaymentServiceTracer) CreatePayout(ctx context.Context, input payment.PayoutInput) (payment.PayoutOutput, error) {
	ctx, span := t.tracer.Start(ctx, "payment.CreatePayout")
	output, err := t.next.CreatePayout(ctx, input)
	finish(span, err)
	return output, err
}

func (t *PaymentServiceTracer) GetPayout(ctx context.Context, query payment.PayoutQuery) (payment.PayoutOutput, error) {
	ctx, span := t.tracer.Start(ctx, "payment.GetPayout")
	output, err := t.next.GetPayout(ctx, query)
	finish(span, err)
	return output, err
}

func (t *PaymentServiceTracer) HandleWebhook(ctx context.Context, input payment.WebhookInput) error {
	ctx, span := t.tracer.Start(ctx, "payment.HandleWebhook")
	err := t.next.HandleWebhook(ctx, input)
	finish(span, err)
	return err
}

func (t *PaymentServiceTracer) Sync(ctx context.Context, limit int) (payment.SyncResult, error) {
	ctx, span := t.tracer.Start(ctx, "payment.Sync")
	output, err := t.next.Sync(ctx, limit)
	finish(span, err)
	return output, err
}
