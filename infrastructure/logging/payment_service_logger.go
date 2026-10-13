package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/payment"
)

const visibleSuffixRunes = 4

type paymentService interface {
	CreateCharge(ctx context.Context, input payment.ChargeInput) (payment.ChargeOutput, error)
	GetCharge(ctx context.Context, query payment.ChargeQuery) (payment.ChargeOutput, error)
	CreatePayout(ctx context.Context, input payment.PayoutInput) (payment.PayoutOutput, error)
	GetPayout(ctx context.Context, query payment.PayoutQuery) (payment.PayoutOutput, error)
	HandleWebhook(ctx context.Context, input payment.WebhookInput) error
	Sync(ctx context.Context, limit int) (payment.SyncResult, error)
}

type PaymentServiceLogger struct {
	next   paymentService
	logger *slog.Logger
}

func NewPaymentServiceLogger(next paymentService, logger *slog.Logger) *PaymentServiceLogger {
	return &PaymentServiceLogger{next: next, logger: logger}
}

func (l *PaymentServiceLogger) CreateCharge(ctx context.Context, input payment.ChargeInput) (payment.ChargeOutput, error) {
	op := begin(l.logger, "payment.CreateCharge")
	output, err := l.next.CreateCharge(ctx, input)
	op.finish(ctx, err,
		slog.String("owner_id", input.OwnerID),
		slog.String("account_id", input.AccountID),
		slog.String("payer_document", lastFour(input.PayerDocument)),
	)
	return output, err
}

func (l *PaymentServiceLogger) GetCharge(ctx context.Context, query payment.ChargeQuery) (payment.ChargeOutput, error) {
	op := begin(l.logger, "payment.GetCharge")
	output, err := l.next.GetCharge(ctx, query)
	op.finish(ctx, err, slog.String("owner_id", query.OwnerID), slog.String("charge_id", query.ChargeID))
	return output, err
}

func (l *PaymentServiceLogger) CreatePayout(ctx context.Context, input payment.PayoutInput) (payment.PayoutOutput, error) {
	op := begin(l.logger, "payment.CreatePayout")
	output, err := l.next.CreatePayout(ctx, input)
	op.finish(ctx, err,
		slog.String("owner_id", input.OwnerID),
		slog.String("account_id", input.AccountID),
		slog.String("pix_key", lastFour(input.PixKey)),
	)
	return output, err
}

func (l *PaymentServiceLogger) GetPayout(ctx context.Context, query payment.PayoutQuery) (payment.PayoutOutput, error) {
	op := begin(l.logger, "payment.GetPayout")
	output, err := l.next.GetPayout(ctx, query)
	op.finish(ctx, err, slog.String("owner_id", query.OwnerID), slog.String("payout_id", query.PayoutID))
	return output, err
}

func (l *PaymentServiceLogger) HandleWebhook(ctx context.Context, input payment.WebhookInput) error {
	op := begin(l.logger, "payment.HandleWebhook")
	err := l.next.HandleWebhook(ctx, input)
	op.finish(ctx, err, slog.String("provider", input.Provider))
	return err
}

func (l *PaymentServiceLogger) Sync(ctx context.Context, limit int) (payment.SyncResult, error) {
	op := begin(l.logger, "payment.Sync")
	output, err := l.next.Sync(ctx, limit)
	op.finish(ctx, err, slog.Int("checked", output.Checked), slog.Int("changed", output.Changed), slog.Int("retried", output.Retried))
	return output, err
}

func lastFour(value string) string {
	runes := []rune(value)
	if len(runes) <= visibleSuffixRunes {
		return string(runes)
	}
	return string(runes[len(runes)-visibleSuffixRunes:])
}
