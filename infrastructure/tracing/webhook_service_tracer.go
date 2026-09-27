package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/services/webhook"
)

type webhookService interface {
	Register(ctx context.Context, input webhook.RegisterInput) (webhook.RegisteredEndpoint, error)
	ListEndpoints(ctx context.Context, ownerID string) ([]webhook.EndpointOutput, error)
	Disable(ctx context.Context, query webhook.EndpointQuery) error
	ListDeliveries(ctx context.Context, input webhook.DeliveryListInput) (webhook.DeliveryPage, error)
	Replay(ctx context.Context, query webhook.DeliveryQuery) (webhook.DeliveryOutput, error)
}

type WebhookServiceTracer struct {
	next   webhookService
	tracer trace.Tracer
}

func NewWebhookServiceTracer(next webhookService, tracer trace.Tracer) *WebhookServiceTracer {
	return &WebhookServiceTracer{next: next, tracer: tracer}
}

func (t *WebhookServiceTracer) Register(ctx context.Context, input webhook.RegisterInput) (webhook.RegisteredEndpoint, error) {
	ctx, span := t.tracer.Start(ctx, "webhook.Register")
	output, err := t.next.Register(ctx, input)
	endSpan(span, err)
	return output, err
}

func (t *WebhookServiceTracer) ListEndpoints(ctx context.Context, ownerID string) ([]webhook.EndpointOutput, error) {
	ctx, span := t.tracer.Start(ctx, "webhook.ListEndpoints")
	output, err := t.next.ListEndpoints(ctx, ownerID)
	endSpan(span, err)
	return output, err
}

func (t *WebhookServiceTracer) Disable(ctx context.Context, query webhook.EndpointQuery) error {
	ctx, span := t.tracer.Start(ctx, "webhook.Disable")
	err := t.next.Disable(ctx, query)
	endSpan(span, err)
	return err
}

func (t *WebhookServiceTracer) ListDeliveries(ctx context.Context, input webhook.DeliveryListInput) (webhook.DeliveryPage, error) {
	ctx, span := t.tracer.Start(ctx, "webhook.ListDeliveries")
	output, err := t.next.ListDeliveries(ctx, input)
	endSpan(span, err)
	return output, err
}

func (t *WebhookServiceTracer) Replay(ctx context.Context, query webhook.DeliveryQuery) (webhook.DeliveryOutput, error) {
	ctx, span := t.tracer.Start(ctx, "webhook.Replay")
	output, err := t.next.Replay(ctx, query)
	endSpan(span, err)
	return output, err
}
