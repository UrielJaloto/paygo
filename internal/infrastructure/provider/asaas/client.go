package asaas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
)

const (
	providerName     = "asaas"
	requestTimeout   = 10 * time.Second
	maximumResponseBytes = 1 << 20
)

type CustomerCache interface {
	Find(ctx context.Context, provider, document string) (string, error)
	Save(ctx context.Context, provider, document, customerID string) error
}

type Provider struct {
	baseURL      string
	apiKey       string
	webhookToken string
	customers    CustomerCache
	http         *http.Client
}

type apiCall struct {
	method string
	path   string
	input     any
	out    any
}

type errorResponse struct {
	Errors []struct {
		Description string `json:"description"`
	} `json:"errors"`
}

func NewMoney(baseURL, apiKey, webhookToken string, customers CustomerCache, httpClient *http.Client) *Provider {
	client := *httpClient
	client.Timeout = requestTimeout
	return &Provider{baseURL: baseURL, apiKey: apiKey, webhookToken: webhookToken, customers: customers, http: &client}
}

func (p *Provider) Name() string {
	return providerName
}

func (p *Provider) sendCall(ctx context.Context, call apiCall) error {
	request, err := p.buildRequest(ctx, call)
	if err != nil {
		return err
	}
	response, err := p.http.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %s %s: %v", payment.ErrProviderUnavailable, call.method, call.path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: reading %s: %v", payment.ErrProviderUnavailable, call.path, err)
	}
	if err := statusError(response.StatusCode, body); err != nil {
		return fmt.Errorf("%s %s: %w", call.method, call.path, err)
	}
	if err := json.Unmarshal(body, call.out); err != nil {
		return fmt.Errorf("%w: decoding %s: %v", payment.ErrProviderUnavailable, call.path, err)
	}
	return nil
}

func (p *Provider) buildRequest(ctx context.Context, call apiCall) (*http.Request, error) {
	body, err := encodeBody(call.input)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", call.path, err)
	}
	request, err := http.NewRequestWithContext(ctx, call.method, p.baseURL+call.path, body)
	if err != nil {
		return nil, fmt.Errorf("building %s: %w", call.path, err)
	}
	request.Header.Set("access_token", p.apiKey)
	request.Header.Set("User-Agent", "paygo")
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func encodeBody(input any) (io.Reader, error) {
	if input == nil {
		return http.NoBody, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(encoded), nil
}

func statusError(status int, body []byte) error {
	if status >= http.StatusInternalServerError || status == http.StatusTooManyRequests {
		return fmt.Errorf("%w: status %d", payment.ErrProviderUnavailable, status)
	}
	if status < http.StatusBadRequest {
		return nil
	}
	description := describeErrors(body)
	if mentionsPixKey(description) {
		return fmt.Errorf("%w: %s", payment.ErrInvalidPixKey, description)
	}
	return fmt.Errorf("%w: status %d: %s", payment.ErrProviderRejected, status, description)
}

func describeErrors(body []byte) string {
	var parsed errorResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "unreadable error response"
	}
	descriptions := make([]string, 0, len(parsed.Errors))
	for _, item := range parsed.Errors {
		descriptions = append(descriptions, item.Description)
	}
	return strings.Join(descriptions, "; ")
}

func mentionsPixKey(description string) bool {
	lowered := strings.ToLower(description)
	return strings.Contains(lowered, "chave pix") || strings.Contains(lowered, "pix key") || strings.Contains(lowered, "pixaddresskey")
}
