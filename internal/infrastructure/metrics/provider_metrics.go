package metrics

import (
	"context"
	"errors"
	"net/http"

	domainpayment "github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/services/payment"
)

type paymentProvider interface {
	Name() string
	CreatePixCharge(ctx context.Context, request payment.ChargeRequest) (payment.ChargeResult, error)
	SendPixPayout(ctx context.Context, request payment.PayoutRequest) (payment.PayoutResult, error)
	FetchCharge(ctx context.Context, providerID string) (domainpayment.EventKind, error)
	FetchPayout(ctx context.Context, providerID string) (domainpayment.EventKind, error)
	ParseWebhook(ctx context.Context, header http.Header, body []byte) (domainpayment.ProviderEvent, error)
}

type ProviderMetrics struct {
	next     paymentProvider
	registry *Registry
}

func NewProviderMetrics(next paymentProvider, registry *Registry) *ProviderMetrics {
	return &ProviderMetrics{next: next, registry: registry}
}

func (m *ProviderMetrics) Name() string {
	return m.next.Name()
}

func (m *ProviderMetrics) CreatePixCharge(ctx context.Context, request payment.ChargeRequest) (payment.ChargeResult, error) {
	result, err := m.next.CreatePixCharge(ctx, request)
	m.count("create_charge", err)
	return result, err
}

func (m *ProviderMetrics) SendPixPayout(ctx context.Context, request payment.PayoutRequest) (payment.PayoutResult, error) {
	result, err := m.next.SendPixPayout(ctx, request)
	m.count("send_payout", err)
	return result, err
}

func (m *ProviderMetrics) FetchCharge(ctx context.Context, providerID string) (domainpayment.EventKind, error) {
	kind, err := m.next.FetchCharge(ctx, providerID)
	m.count("fetch_charge", err)
	return kind, err
}

func (m *ProviderMetrics) FetchPayout(ctx context.Context, providerID string) (domainpayment.EventKind, error) {
	kind, err := m.next.FetchPayout(ctx, providerID)
	m.count("fetch_payout", err)
	return kind, err
}

func (m *ProviderMetrics) ParseWebhook(ctx context.Context, header http.Header, body []byte) (domainpayment.ProviderEvent, error) {
	occurred, err := m.next.ParseWebhook(ctx, header, body)
	m.count("parse_webhook", err)
	return occurred, err
}

func (m *ProviderMetrics) count(operation string, err error) {
	m.registry.providerRequests.WithLabelValues(m.next.Name(), operation, resolveProviderResult(err)).Inc()
}

func resolveProviderResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, domainpayment.ErrProviderRejected), errors.Is(err, domainpayment.ErrWebhookUnauthorized):
		return "rejected"
	default:
		return "unavailable"
	}
}
