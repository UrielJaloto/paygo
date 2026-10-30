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

	"github.com/UrielJaloto/paygo/domain/payment"
)

const (
	providerName     = "asaas"
	requestTimeout   = 10 * time.Second
	maxResponseBytes = 1 << 20
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
	in     any
	out    any
}

type errorResponse struct {
	Errors []struct {
		Description string `json:"description"`
	} `json:"errors"`
}

func New(baseURL, apiKey, webhookToken string, customers CustomerCache, httpClient *http.Client) *Provider {
	client := *httpClient
	client.Timeout = requestTimeout
	return &Provider{baseURL: baseURL, apiKey: apiKey, webhookToken: webhookToken, customers: customers, http: &client}
}

func (p *Provider) Name() string {
	return providerName
}

func (p *Provider) do(ctx context.Context, call apiCall) error {
	req, err := p.newRequest(ctx, call)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s %s: %v", payment.ErrProviderUnavailable, call.method, call.path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("%w: reading %s: %v", payment.ErrProviderUnavailable, call.path, err)
	}
	if err := statusError(resp.StatusCode, body); err != nil {
		return fmt.Errorf("%s %s: %w", call.method, call.path, err)
	}
	if err := json.Unmarshal(body, call.out); err != nil {
		return fmt.Errorf("%w: decoding %s: %v", payment.ErrProviderUnavailable, call.path, err)
	}
	return nil
}

func (p *Provider) newRequest(ctx context.Context, call apiCall) (*http.Request, error) {
	body, err := encodeBody(call.in)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", call.path, err)
	}
	req, err := http.NewRequestWithContext(ctx, call.method, p.baseURL+call.path, body)
	if err != nil {
		return nil, fmt.Errorf("building %s: %w", call.path, err)
	}
	req.Header.Set("access_token", p.apiKey)
	req.Header.Set("User-Agent", "paygo")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func encodeBody(in any) (io.Reader, error) {
	if in == nil {
		return http.NoBody, nil
	}
	encoded, err := json.Marshal(in)
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
	description := describe(body)
	if mentionsPixKey(description) {
		return fmt.Errorf("%w: %s", payment.ErrInvalidPixKey, description)
	}
	return fmt.Errorf("%w: status %d: %s", payment.ErrProviderRejected, status, description)
}

func describe(body []byte) string {
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
