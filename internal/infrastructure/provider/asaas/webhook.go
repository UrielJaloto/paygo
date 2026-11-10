package asaas

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
)

const webhookTokenHeader = "asaas-access-token"

type webhookResource struct {
	ID string `json:"id"`
}

type webhookBody struct {
	ID       string          `json:"id"`
	Event    string          `json:"event"`
	Payment  webhookResource `json:"payment"`
	Transfer webhookResource `json:"transfer"`
}

func (p *Provider) ParseWebhook(_ context.Context, header http.Header, body []byte) (payment.ProviderEvent, error) {
	if subtle.ConstantTimeCompare([]byte(header.Get(webhookTokenHeader)), []byte(p.webhookToken)) != 1 {
		return payment.ProviderEvent{}, payment.ErrWebhookUnauthorized
	}
	var parsed webhookBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return payment.ProviderEvent{}, fmt.Errorf("%w: decoding asaas webhook: %v", payment.ErrProviderRejected, err)
	}
	providerID := parsed.Payment.ID
	if providerID == "" {
		providerID = parsed.Transfer.ID
	}
	return payment.ProviderEvent{Provider: providerName, EventID: parsed.ID, ProviderID: providerID, Kind: parseWebhookKind(parsed.Event)}, nil
}

func parseWebhookKind(event string) string {
	switch event {
	case "PAYMENT_RECEIVED", "PAYMENT_CONFIRMED":
		return payment.EventChargePaid
	case "PAYMENT_OVERDUE":
		return payment.EventChargeExpired
	case "TRANSFER_DONE":
		return payment.EventPayoutDone
	case "TRANSFER_FAILED", "TRANSFER_CANCELLED":
		return payment.EventPayoutFailed
	}
	return payment.EventIgnored
}
