package asaas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/UrielJaloto/paygo/domain/payment"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

const operationTypePix = "PIX"

type transferRequest struct {
	Value             json.Number `json:"value"`
	PixAddressKey     string      `json:"pixAddressKey"`
	PixAddressKeyType string      `json:"pixAddressKeyType"`
	OperationType     string      `json:"operationType"`
	ExternalReference string      `json:"externalReference"`
}

type transferResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (p *Provider) SendPixPayout(ctx context.Context, request paymentservice.PayoutRequest) (paymentservice.PayoutResult, error) {
	var created transferResponse
	err := p.sendCall(ctx, apiCall{method: http.MethodPost, path: "/transfers", input: transferRequest{
		Value:             decimalValue(request.AmountMinor),
		PixAddressKey:     request.PixKey,
		PixAddressKeyType: strings.ToUpper(request.PixKeyType),
		OperationType:     operationTypePix,
		ExternalReference: request.ExternalReference,
	}, out: &created})
	if err != nil {
		return paymentservice.PayoutResult{}, err
	}
	return paymentservice.PayoutResult{ProviderID: created.ID}, nil
}

func (p *Provider) GetPayout(ctx context.Context, providerID string) (payment.EventKind, error) {
	var found transferResponse
	path := "/transfers/" + url.PathEscape(providerID)
	if err := p.sendCall(ctx, apiCall{method: http.MethodGet, path: path, out: &found}); err != nil {
		return "", err
	}
	return payoutKind(found.Status), nil
}

func payoutKind(status string) payment.EventKind {
	switch status {
	case "DONE":
		return payment.EventPayoutDone
	case "FAILED", "CANCELLED":
		return payment.EventPayoutFailed
	}
	return payment.EventIgnored
}
