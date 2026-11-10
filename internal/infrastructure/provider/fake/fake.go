package fake

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
)

const (
	providerName = "fake"
	tokenHeader  = "Paygo-Fake-Token"
	idPrefix     = "fake_"
	pixPrefix    = "00020126fake"
	onePixelPNG  = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
)

type IDGenerator interface {
	NewID() string
}

type Provider struct {
	token   string
	ids     IDGenerator
	mutex   sync.Mutex
	charges map[string]string
	payouts map[string]string
}

type webhookBody struct {
	EventID    string `json:"event_id"`
	Type       string `json:"type"`
	ProviderID string `json:"provider_id"`
	SyncOnly   bool   `json:"sync_only"`
}

func NewProvider(token string, ids IDGenerator) *Provider {
	return &Provider{
		token:   token,
		ids:     ids,
		charges: make(map[string]string),
		payouts: make(map[string]string),
	}
}

func (p *Provider) Name() string {
	return providerName
}

func (p *Provider) CreatePixCharge(context.Context, paymentservice.ChargeRequest) (paymentservice.ChargeResult, error) {
	id := idPrefix + p.ids.NewID()
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.charges[id] = payment.EventIgnored
	return paymentservice.ChargeResult{ProviderID: id, PixCopyPaste: pixPrefix + id, PixQRBase64: onePixelPNG}, nil
}

func (p *Provider) SendPixPayout(context.Context, paymentservice.PayoutRequest) (paymentservice.PayoutResult, error) {
	id := idPrefix + p.ids.NewID()
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.payouts[id] = payment.EventIgnored
	return paymentservice.PayoutResult{ProviderID: id}, nil
}

func (p *Provider) FetchCharge(_ context.Context, providerID string) (string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return findStatus(p.charges, providerID), nil
}

func (p *Provider) FetchPayout(_ context.Context, providerID string) (string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return findStatus(p.payouts, providerID), nil
}

func (p *Provider) MarkPaid(providerID string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.charges[providerID] = payment.EventChargePaid
}

func (p *Provider) MarkPayoutCompleted(providerID string) {
	p.setPayoutState(providerID, payment.EventPayoutDone)
}

func (p *Provider) MarkPayoutFailed(providerID string) {
	p.setPayoutState(providerID, payment.EventPayoutFailed)
}

func (p *Provider) setPayoutState(providerID string, kind string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.payouts[providerID] = kind
}

func (p *Provider) ParseWebhook(_ context.Context, header http.Header, body []byte) (payment.ProviderEvent, error) {
	if subtle.ConstantTimeCompare([]byte(header.Get(tokenHeader)), []byte(p.token)) != 1 {
		return payment.ProviderEvent{}, payment.ErrWebhookUnauthorized
	}
	var parsed webhookBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return payment.ProviderEvent{}, fmt.Errorf("%w: decoding fake webhook: %v", payment.ErrProviderRejected, err)
	}
	kind := parseEventKind(parsed.Type)
	p.recordState(parsed.ProviderID, kind)
	if parsed.SyncOnly {
		kind = payment.EventIgnored
	}
	return payment.ProviderEvent{Provider: providerName, EventID: parsed.EventID, ProviderID: parsed.ProviderID, Kind: kind}, nil
}

func (p *Provider) recordState(providerID string, kind string) {
	switch kind {
	case payment.EventChargePaid:
		p.MarkPaid(providerID)
	case payment.EventPayoutDone, payment.EventPayoutFailed:
		p.setPayoutState(providerID, kind)
	}
}

func parseEventKind(webhookType string) string {
	switch webhookType {
	case "charge.paid":
		return payment.EventChargePaid
	case "payout.done":
		return payment.EventPayoutDone
	case "payout.failed":
		return payment.EventPayoutFailed
	}
	return payment.EventIgnored
}

func findStatus(states map[string]string, providerID string) string {
	if kind, found := states[providerID]; found {
		return kind
	}
	return payment.EventIgnored
}
