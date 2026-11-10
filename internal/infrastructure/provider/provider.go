package provider

import (
	"net/http"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/config"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/asaas"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/fake"
	"github.com/UrielJaloto/paygo/internal/services/payment"
)

const clientTimeout = 30 * time.Second

func NewMoney(settings config.Payments, customers asaas.CustomerCache, ids fake.IDGenerator) payment.PaymentProvider {
	if settings.Provider == config.ProviderAsaas {
		client := &http.Client{Timeout: clientTimeout}
		return asaas.NewMoney(settings.Asaas.BaseURL, settings.Asaas.APIKey, settings.Asaas.WebhookToken, customers, client)
	}
	return fake.NewMoney(settings.FakeWebhookToken, ids)
}
