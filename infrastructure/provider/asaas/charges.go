package asaas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/UrielJaloto/paygo/domain/payment"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

const (
	billingTypePix = "PIX"
	dueDateLayout  = "2006-01-02"
)

type customerRequest struct {
	Name    string `json:"name"`
	CPFCNPJ string `json:"cpfCnpj"`
}

type customerResponse struct {
	ID string `json:"id"`
}

type paymentRequest struct {
	Customer          string      `json:"customer"`
	BillingType       string      `json:"billingType"`
	Value             json.Number `json:"value"`
	DueDate           string      `json:"dueDate"`
	ExternalReference string      `json:"externalReference"`
}

type paymentResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type pixQRCodeResponse struct {
	EncodedImage string `json:"encodedImage"`
	Payload      string `json:"payload"`
}

func (p *Provider) CreatePixCharge(ctx context.Context, request paymentservice.ChargeRequest) (paymentservice.ChargeResult, error) {
	customerID, err := p.customerFor(ctx, request)
	if err != nil {
		return paymentservice.ChargeResult{}, err
	}
	var created paymentResponse
	err = p.sendCall(ctx, apiCall{method: http.MethodPost, path: "/payments", input: paymentRequest{
		Customer:          customerID,
		BillingType:       billingTypePix,
		Value:             decimalValue(request.AmountMinor),
		DueDate:           request.DueDate.Format(dueDateLayout),
		ExternalReference: request.ExternalReference,
	}, out: &created})
	if err != nil {
		return paymentservice.ChargeResult{}, err
	}
	var code pixQRCodeResponse
	path := "/payments/" + url.PathEscape(created.ID) + "/pixQrCode"
	if err := p.sendCall(ctx, apiCall{method: http.MethodGet, path: path, out: &code}); err != nil {
		return paymentservice.ChargeResult{}, err
	}
	return paymentservice.ChargeResult{ProviderID: created.ID, PixCopyPaste: code.Payload, PixQRBase64: code.EncodedImage}, nil
}

func (p *Provider) GetCharge(ctx context.Context, providerID string) (payment.EventKind, error) {
	var found paymentResponse
	path := "/payments/" + url.PathEscape(providerID)
	if err := p.sendCall(ctx, apiCall{method: http.MethodGet, path: path, out: &found}); err != nil {
		return "", err
	}
	return chargeKind(found.Status), nil
}

func (p *Provider) customerFor(ctx context.Context, request paymentservice.ChargeRequest) (string, error) {
	cached, err := p.customers.Find(ctx, providerName, request.PayerDocument)
	if err != nil {
		return "", fmt.Errorf("finding asaas customer: %w", err)
	}
	if cached != "" {
		return cached, nil
	}
	var created customerResponse
	input := customerRequest{Name: request.PayerName, CPFCNPJ: request.PayerDocument}
	if err := p.sendCall(ctx, apiCall{method: http.MethodPost, path: "/customers", input: input, out: &created}); err != nil {
		return "", err
	}
	if err := p.customers.Save(ctx, providerName, request.PayerDocument, created.ID); err != nil {
		return "", fmt.Errorf("saving asaas customer: %w", err)
	}
	return created.ID, nil
}

func chargeKind(status string) payment.EventKind {
	switch status {
	case "RECEIVED", "CONFIRMED":
		return payment.EventChargePaid
	case "OVERDUE":
		return payment.EventChargeExpired
	}
	return payment.EventIgnored
}

func decimalValue(amountMinor int64) json.Number {
	return json.Number(fmt.Sprintf("%d.%02d", amountMinor/100, amountMinor%100))
}
