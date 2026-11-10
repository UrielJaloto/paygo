package rest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/presentation/rest"
)

type stubParser struct {
	err      error
	received string
}

func (s *stubParser) ParseWebhook(_ context.Context, _ http.Header, body []byte) (payment.ProviderEvent, error) {
	s.received = string(body)
	return payment.ProviderEvent{}, s.err
}

func newWorkerRouter(parser rest.WebhookParser) http.Handler {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return rest.NewWorkerRouter(&spyPanics{}, rest.WorkerControllers{
		Health:        rest.NewHealthController(stubReadiness(true), metrics),
		ProviderState: rest.NewProviderStateController(parser),
	})
}

func TestWorkerRouter_ProviderState(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "recorded", want: http.StatusOK},
		{name: "bad token", err: payment.ErrWebhookUnauthorized, want: http.StatusUnauthorized},
		{name: "unreadable event", err: fmt.Errorf("%w: malformed", payment.ErrProviderRejected), want: http.StatusUnprocessableEntity},
		{name: "unexpected failure", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			parser := &stubParser{err: testCase.err}
			recorder := httptest.NewRecorder()

			newWorkerRouter(parser).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/provider/v1/webhooks/fake", strings.NewReader(`{"type":"charge.paid"}`)))

			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.want)
			}
			if parser.received != `{"type":"charge.paid"}` {
				t.Fatalf("parsed body = %q", parser.received)
			}
		})
	}
}

func TestWorkerRouter_ProviderState_BodyTooLarge_ReturnsPayloadTooLarge(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := strings.NewReader(strings.Repeat("a", 256<<10+1))

	newWorkerRouter(&stubParser{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/provider/v1/webhooks/fake", body))

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWorkerRouter_HealthAndUnknownRoute(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: "/healthz", want: http.StatusOK},
		{path: "/readyz", want: http.StatusOK},
		{path: "/metrics", want: http.StatusOK},
		{path: "/v1/accounts", want: http.StatusNotFound},
	}
	for _, testCase := range tests {
		t.Run(testCase.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			newWorkerRouter(&stubParser{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, testCase.path, nil))

			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.want)
			}
		})
	}
}
