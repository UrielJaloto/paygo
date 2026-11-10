package payment

import (
	"context"
	"net/http"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
)

type PaymentProvider interface {
	Name() string
	CreatePixCharge(ctx context.Context, request ChargeRequest) (ChargeResult, error)
	SendPixPayout(ctx context.Context, request PayoutRequest) (PayoutResult, error)
	FetchCharge(ctx context.Context, providerID string) (payment.EventKind, error)
	FetchPayout(ctx context.Context, providerID string) (payment.EventKind, error)
	ParseWebhook(ctx context.Context, header http.Header, body []byte) (payment.ProviderEvent, error)
}

type ChargeRequest struct {
	ExternalReference string
	PayerName         string
	PayerDocument     string
	AmountMinor       int64
	DueDate           time.Time
}

type ChargeResult struct {
	ProviderID   string
	PixCopyPaste string
	PixQRBase64  string
}

type PayoutRequest struct {
	ExternalReference string
	PixKey            string
	PixKeyType        string
	AmountMinor       int64
}

type PayoutResult struct {
	ProviderID string
}
