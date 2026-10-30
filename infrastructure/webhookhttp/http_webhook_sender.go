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

	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/webhook"
)

var ErrUnexpectedStatus = errors.New("unexpected webhook response status")

type Signer interface {
	Sign(secret string, timestamp int64, body []byte) string
}

type Clock interface {
	Now() time.Time
}

type HTTPWebhookSender struct {
	client *http.Client
	signer Signer
	clock  Clock
}

func New(client *http.Client, signer Signer, clock Clock) *HTTPWebhookSender {
	return &HTTPWebhookSender{client: client, signer: signer, clock: clock}
}

func (s *HTTPWebhookSender) Send(ctx context.Context, endpoint webhook.Endpoint, source event.Event) error {
	request, err := s.newRequest(ctx, endpoint, source)
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

func (s *HTTPWebhookSender) newRequest(ctx context.Context, endpoint webhook.Endpoint, source event.Event) (*http.Request, error) {
	body, _ := json.Marshal(toPayload(source))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, string(endpoint.URL()), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := s.clock.Now().Unix()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Paygo-Event-Id", string(source.ID()))
	request.Header.Set("Paygo-Event-Type", string(source.Type()))
	request.Header.Set("Paygo-Timestamp", strconv.FormatInt(timestamp, 10))
	request.Header.Set("Paygo-Signature", s.signer.Sign(endpoint.Secret(), timestamp, body))
	return request, nil
}
