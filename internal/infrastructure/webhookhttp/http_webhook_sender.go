package webhookhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
)

var ErrUnexpectedStatus = errors.New("unexpected webhook response status")

type HTTPWebhookSender struct {
	client *http.Client
	sign   func(secret string, timestamp int64, body []byte) string
	now    func() time.Time
}

func NewHTTPWebhookSender(client *http.Client, sign func(secret string, timestamp int64, body []byte) string, now func() time.Time) *HTTPWebhookSender {
	return &HTTPWebhookSender{client: client, sign: sign, now: now}
}

func (s *HTTPWebhookSender) Send(ctx context.Context, endpoint webhook.Endpoint, source event.Event) error {
	request, err := s.buildRequest(ctx, endpoint, source)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: %d", ErrUnexpectedStatus, response.StatusCode)
	}
	return nil
}

func (s *HTTPWebhookSender) buildRequest(ctx context.Context, endpoint webhook.Endpoint, source event.Event) (*http.Request, error) {
	body, encodeErr := json.Marshal(toPayload(source))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, string(endpoint.URL()), bytes.NewReader(body))
	if err := errors.Join(encodeErr, err); err != nil {
		return nil, err
	}
	timestamp := s.now().Unix()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Paygo-Event-Id", string(source.ID()))
	request.Header.Set("Paygo-Event-Type", string(source.Type()))
	request.Header.Set("Paygo-Timestamp", strconv.FormatInt(timestamp, 10))
	request.Header.Set("Paygo-Signature", s.sign(endpoint.Secret(), timestamp, body))
	return request, nil
}
