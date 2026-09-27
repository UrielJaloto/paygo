package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/webhook"
)

type webhookService interface {
	Register(ctx context.Context, input webhook.RegisterInput) (webhook.RegisteredEndpoint, error)
	ListEndpoints(ctx context.Context, ownerID string) ([]webhook.EndpointOutput, error)
	Disable(ctx context.Context, query webhook.EndpointQuery) error
	ListDeliveries(ctx context.Context, input webhook.DeliveryListInput) (webhook.DeliveryPage, error)
	Replay(ctx context.Context, query webhook.DeliveryQuery) (webhook.DeliveryOutput, error)
}

type WebhookServiceLogger struct {
	next   webhookService
	logger *slog.Logger
}

func NewWebhookServiceLogger(next webhookService, logger *slog.Logger) *WebhookServiceLogger {
	return &WebhookServiceLogger{next: next, logger: logger}
}

func (l *WebhookServiceLogger) Register(ctx context.Context, input webhook.RegisterInput) (webhook.RegisteredEndpoint, error) {
	op := begin(l.logger, "webhook.Register")
	output, err := l.next.Register(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID))
	return output, err
}

func (l *WebhookServiceLogger) ListEndpoints(ctx context.Context, ownerID string) ([]webhook.EndpointOutput, error) {
	op := begin(l.logger, "webhook.ListEndpoints")
	output, err := l.next.ListEndpoints(ctx, ownerID)
	op.finish(ctx, err, slog.String("owner_id", ownerID))
	return output, err
}

func (l *WebhookServiceLogger) Disable(ctx context.Context, query webhook.EndpointQuery) error {
	op := begin(l.logger, "webhook.Disable")
	err := l.next.Disable(ctx, query)
	op.finish(ctx, err, slog.String("owner_id", query.OwnerID), slog.String("endpoint_id", query.EndpointID))
	return err
}

func (l *WebhookServiceLogger) ListDeliveries(ctx context.Context, input webhook.DeliveryListInput) (webhook.DeliveryPage, error) {
	op := begin(l.logger, "webhook.ListDeliveries")
	output, err := l.next.ListDeliveries(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID), slog.String("status", input.Status))
	return output, err
}

func (l *WebhookServiceLogger) Replay(ctx context.Context, query webhook.DeliveryQuery) (webhook.DeliveryOutput, error) {
	op := begin(l.logger, "webhook.Replay")
	output, err := l.next.Replay(ctx, query)
	op.finish(ctx, err, slog.String("owner_id", query.OwnerID), slog.String("delivery_id", query.DeliveryID))
	return output, err
}
