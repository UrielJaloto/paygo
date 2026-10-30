package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/UrielJaloto/paygo/infrastructure/provider/fake"
	"github.com/UrielJaloto/paygo/services/payment"
)

const (
	markToken = "fake-token"
	markRoute = "/provider/v1/webhooks/fake"
)

type namedProvider struct {
	payment.PaymentProvider
}

func (namedProvider) Name() string {
	return "asaas"
}

func postMark(handler http.Handler, token, body string) int {
	request := httptest.NewRequest(http.MethodPost, markRoute, strings.NewReader(body))
	request.Header.Set("Paygo-Fake-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestNewMux_FakeMark_ReportsParseOutcome(t *testing.T) {
	tests := []struct {
		name  string
		token string
		body  string
		want  int
	}{
		{name: "valid", token: markToken, body: `{"event_id":"e","type":"charge.paid","provider_id":"p"}`, want: http.StatusOK},
		{name: "bad token", token: "wrong", body: `{}`, want: http.StatusUnauthorized},
		{name: "malformed body", token: markToken, body: `{`, want: http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := newMux(http.NotFoundHandler(), fake.New(markToken, nil))
			if got := postMark(mux, tc.token, tc.body); got != tc.want {
				t.Errorf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewMux_RealProvider_HasNoFakeMark(t *testing.T) {
	mux := newMux(http.NotFoundHandler(), namedProvider{})
	if got := postMark(mux, markToken, `{}`); got != http.StatusNotFound {
		t.Errorf("status = %d, want %d", got, http.StatusNotFound)
	}
}
