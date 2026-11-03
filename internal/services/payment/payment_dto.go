package payment

import (
	"net/http"
	"time"
)

type ChargeInput struct {
	OwnerID        string
	IdempotencyKey string
	AccountID      string
	Amount         string
	Currency       string
	PayerName      string
	PayerDocument  string
}

type ChargeOutput struct {
	ID           string
	AccountID    string
	Status       string
	PixCopyPaste string
	PixQRBase64  string
	AmountMinor  int64
	Currency     string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	IsReplay     bool
}

type ChargeQuery struct {
	OwnerID  string
	ChargeID string
}

type PayoutInput struct {
	OwnerID        string
	IdempotencyKey string
	AccountID      string
	Amount         string
	Currency       string
	PixKey         string
	PixKeyType     string
}

type PayoutOutput struct {
	ID          string
	AccountID   string
	Status      string
	PixKeyType  string
	AmountMinor int64
	Currency    string
	CreatedAt   time.Time
	IsReplay    bool
}

type PayoutQuery struct {
	OwnerID  string
	PayoutID string
}

type SyncResult struct {
	Checked int
	Changed int
	Retried int
}

type WebhookInput struct {
	Provider string
	Header   http.Header
	Body     []byte
}
