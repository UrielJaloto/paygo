package payment

import "errors"

var (
	ErrChargeNotFound      = errors.New("charge not found")
	ErrPayoutNotFound      = errors.New("payout not found")
	ErrInvalidDocument     = errors.New("invalid document")
	ErrInvalidPixKey       = errors.New("invalid pix key")
	ErrInvalidPayerName    = errors.New("invalid payer name")
	ErrInvalidTransition   = errors.New("invalid transition")
	ErrUnsupportedProvider = errors.New("unsupported provider")
	ErrProviderRejected    = errors.New("provider rejected")
	ErrProviderUnavailable = errors.New("provider unavailable")
	ErrWebhookUnauthorized = errors.New("webhook unauthorized")
)
