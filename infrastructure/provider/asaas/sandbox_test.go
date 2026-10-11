//go:build asaas_sandbox

package asaas_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/provider/asaas"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

func TestSandbox_CreatePixCharge_ReturnsQRCode(t *testing.T) {
	key := os.Getenv("PAYGO_ASAAS_API_KEY")
	if key == "" {
		t.Fatal("PAYGO_ASAAS_API_KEY is required")
	}
	provider := asaas.NewMoney("https://api-sandbox.asaas.com/v3", key, "", emptyCache(), &http.Client{})
	result, err := provider.CreatePixCharge(context.Background(), paymentservice.ChargeRequest{
		ExternalReference: "paygo-sandbox",
		PayerName:         "John Doe",
		PayerDocument:     document,
		AmountMinor:       500,
		DueDate:           time.Now().AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("CreatePixCharge: %v", err)
	}
	if result.ProviderID == "" || result.PixCopyPaste == "" || result.PixQRBase64 == "" {
		t.Fatalf("result = %+v", result)
	}
}
