package metrics

import (
	"context"
	"errors"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	domainpayment "github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	"github.com/UrielJaloto/paygo/internal/services/payment"
)

type paymentService interface {
	CreateCharge(ctx context.Context, input payment.ChargeInput) (payment.ChargeOutput, error)
	GetCharge(ctx context.Context, query payment.ChargeQuery) (payment.ChargeOutput, error)
	CreatePayout(ctx context.Context, input payment.PayoutInput) (payment.PayoutOutput, error)
	GetPayout(ctx context.Context, query payment.PayoutQuery) (payment.PayoutOutput, error)
	HandleWebhook(ctx context.Context, input payment.WebhookInput) error
	Sync(ctx context.Context, limit int) (payment.SyncResult, error)
}

type PaymentServiceMetrics struct {
	next     paymentService
	registry *Registry
}

func NewPaymentServiceMetrics(next paymentService, registry *Registry) *PaymentServiceMetrics {
	return &PaymentServiceMetrics{next: next, registry: registry}
}

func (m *PaymentServiceMetrics) CreateCharge(ctx context.Context, input payment.ChargeInput) (payment.ChargeOutput, error) {
	output, err := m.next.CreateCharge(ctx, input)
	m.count("charge", paymentStatus(output.Status, err))
	return output, err
}

func (m *PaymentServiceMetrics) GetCharge(ctx context.Context, query payment.ChargeQuery) (payment.ChargeOutput, error) {
	return m.next.GetCharge(ctx, query)
}

func (m *PaymentServiceMetrics) CreatePayout(ctx context.Context, input payment.PayoutInput) (payment.PayoutOutput, error) {
	output, err := m.next.CreatePayout(ctx, input)
	m.count("payout", paymentStatus(output.Status, err))
	return output, err
}

func (m *PaymentServiceMetrics) GetPayout(ctx context.Context, query payment.PayoutQuery) (payment.PayoutOutput, error) {
	return m.next.GetPayout(ctx, query)
}

func (m *PaymentServiceMetrics) HandleWebhook(ctx context.Context, input payment.WebhookInput) error {
	return m.next.HandleWebhook(ctx, input)
}

func (m *PaymentServiceMetrics) Sync(ctx context.Context, limit int) (payment.SyncResult, error) {
	return m.next.Sync(ctx, limit)
}

func (m *PaymentServiceMetrics) count(kind, status string) {
	m.registry.payments.WithLabelValues(kind, status).Inc()
}

func paymentStatus(status string, err error) string {
	if err == nil {
		return status
	}
	for _, rejection := range paymentRejections() {
		if errors.Is(err, rejection) {
			return "rejected"
		}
	}
	return "error"
}

func paymentRejections() []error {
	return []error{
		account.ErrInvalidAccountID, account.ErrAccountNotFound, account.ErrAccountFrozen,
		money.ErrUnsupportedCurrency, money.ErrInvalidAmount, money.ErrAmountTooLarge, money.ErrCurrencyMismatch,
		domainpayment.ErrInvalidDocument, domainpayment.ErrInvalidPixKey, domainpayment.ErrInvalidPayerName,
		domainpayment.ErrUnsupportedProvider, domainpayment.ErrProviderRejected,
		transfer.ErrInsufficientFunds, transfer.ErrIdempotencyConflict, transfer.ErrInvalidIdempotencyKey,
	}
}
