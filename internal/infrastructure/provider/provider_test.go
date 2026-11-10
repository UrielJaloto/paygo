package provider_test

import (
	"context"
	"testing"

	"github.com/UrielJaloto/paygo/internal/infrastructure/config"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider"
)

type stubCustomers struct{}

func (stubCustomers) Find(context.Context, string, string) (string, error) { return "", nil }

func (stubCustomers) Save(context.Context, string, string, string) error { return nil }

type stubIDs struct{}

func (stubIDs) NewID() string { return "id" }

func TestNewSelectedProvider_SelectsProviderBySettings(t *testing.T) {
	tests := []struct {
		name     string
		settings config.Payments
		want     string
	}{
		{name: "fake", settings: config.Payments{Provider: config.ProviderFake, FakeWebhookToken: "token"}, want: "fake"},
		{name: "asaas", settings: config.Payments{Provider: config.ProviderAsaas, Asaas: config.Asaas{BaseURL: "https://example.test", APIKey: "key"}}, want: "asaas"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := provider.NewSelectedProvider(testCase.settings, stubCustomers{}, stubIDs{})

			if got.Name() != testCase.want {
				t.Fatalf("Name() = %q, want %q", got.Name(), testCase.want)
			}
		})
	}
}
