package webhookhttp_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/webhookhttp"
)

const goldenBody = `{"id":"evt-1","type":"transfer.completed","created_at":"2026-01-02T03:04:05Z",` +
	`"data":{"id":"trf-1","kind":"transfer","status":"completed","from_account_id":"acc-1",` +
	`"to_account_id":"acc-2","amount":"12.34","currency":"BRL","created_at":"2026-01-02T03:04:00Z"}}`

var now = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return now
}

type spySigner struct {
	secret    string
	timestamp int64
	body      []byte
}

func (s *spySigner) SignWebhook(secret string, timestamp int64, body []byte) string {
	s.secret, s.timestamp, s.body = secret, timestamp, body
	return "signature"
}

func fixedEvent() event.Event {
	source := transfer.Restore(transfer.Snapshot{
		ID: "trf-1",
		Request: transfer.Request{
			Kind:   transfer.KindTransfer,
			From:   "acc-1",
			To:     "acc-2",
			Amount: money.NewMoney(1234, money.BRL),
		},
		Status:    transfer.StatusCompleted,
		CreatedAt: now.Add(-5 * time.Second),
	})
	return event.NewTransferEvent(event.Metadata{ID: "evt-1", At: now}, source, nil)
}

func endpointAt(url string) webhook.Endpoint {
	return webhook.RestoreEndpoint(webhook.EndpointSnapshot{
		ID:     "end-1",
		URL:    webhook.EndpointURL(url),
		Secret: "secret",
	})
}

func newSender(client *http.Client) *webhookhttp.HTTPWebhookSender {
	return webhookhttp.NewMoney(client, &spySigner{}, fixedClock{})
}

func respondingWith(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
	}))
}

func TestSend_Status_MapsToResult(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: 200},
		{status: 204},
		{status: 299},
		{status: 300, want: webhookhttp.ErrUnexpectedStatus},
		{status: 404, want: webhookhttp.ErrUnexpectedStatus},
		{status: 500, want: webhookhttp.ErrUnexpectedStatus},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			server := respondingWith(test.status)
			defer server.Close()

			err := newSender(server.Client()).Send(context.Background(), endpointAt(server.URL), fixedEvent())

			if !errors.Is(err, test.want) {
				t.Fatalf("Send() error = %v, want %v", err, test.want)
			}
			if test.want != nil && !strings.Contains(err.Error(), strconv.Itoa(test.status)) {
				t.Fatalf("Send() error = %v, want status code", err)
			}
		})
	}
}

func TestSend_Request_SendsSignedPayload(t *testing.T) {
	var method string
	var header http.Header
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		method, header = request.Method, request.Header
		body, _ = io.ReadAll(request.Body)
	}))
	defer server.Close()
	signer := &spySigner{}

	err := webhookhttp.NewMoney(server.Client(), signer, fixedClock{}).Send(context.Background(), endpointAt(server.URL), fixedEvent())

	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("method = %s, want POST", method)
	}
	if string(body) != goldenBody {
		t.Fatalf("body = %s, want %s", body, goldenBody)
	}
	if signer.secret != "secret" || signer.timestamp != now.Unix() || !bytes.Equal(signer.body, body) {
		t.Fatalf("signer received %q, %d, %s", signer.secret, signer.timestamp, signer.body)
	}
	wantHeaders := map[string]string{
		"Content-Type":     "application/json",
		"Paygo-Event-Id":   "evt-1",
		"Paygo-Event-Type": "transfer.completed",
		"Paygo-Timestamp":  strconv.FormatInt(now.Unix(), 10),
		"Paygo-Signature":  "signature",
	}
	for name, want := range wantHeaders {
		if got := header.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}

func TestSend_ServerTooSlow_ReturnsTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	client := server.Client()
	client.Timeout = 50 * time.Millisecond

	err := newSender(client).Send(context.Background(), endpointAt(server.URL), fixedEvent())

	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("Send() error = %v, want a timeout", err)
	}
}

func TestSend_ServerClosed_ReturnsWrappedError(t *testing.T) {
	server := respondingWith(http.StatusOK)
	client := server.Client()
	url := server.URL
	server.Close()

	err := newSender(client).Send(context.Background(), endpointAt(url), fixedEvent())

	if err == nil || !strings.HasPrefix(err.Error(), "sending webhook: ") {
		t.Fatalf("Send() error = %v, want wrapped transport error", err)
	}
}

func TestSend_ContextCancelled_ReturnsCanceled(t *testing.T) {
	server := respondingWith(http.StatusOK)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := newSender(server.Client()).Send(ctx, endpointAt(server.URL), fixedEvent())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want %v", err, context.Canceled)
	}
}

func TestSend_InvalidURL_ReturnsWrappedError(t *testing.T) {
	err := newSender(http.DefaultClient).Send(context.Background(), endpointAt("://invalid"), fixedEvent())

	if err == nil || !strings.HasPrefix(err.Error(), "sending webhook: ") {
		t.Fatalf("Send() error = %v, want wrapped request error", err)
	}
}

func TestSend_LargeResponse_ReusesConnection(t *testing.T) {
	remotes := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		remotes <- request.RemoteAddr
		_, _ = writer.Write(make([]byte, 1<<20))
	}))
	defer server.Close()
	sender := newSender(server.Client())

	for attempt := range 2 {
		if err := sender.Send(context.Background(), endpointAt(server.URL), fixedEvent()); err != nil {
			t.Fatalf("Send() attempt %d error = %v", attempt, err)
		}
	}

	first, second := <-remotes, <-remotes
	if first != second {
		t.Fatalf("connections = %s and %s, want reuse", first, second)
	}
}
