package webhook_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/webhook"
)

var (
	validSecret = "whsec_" + strings.Repeat("a", 43)
	createdAt   = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func TestParseEndpointURL_AcceptsAllowedTargets(t *testing.T) {
	base := "https://example.com/"
	cases := []struct {
		name string
		text string
	}{
		{name: "https", text: "https://example.com/hook"},
		{name: "http localhost", text: "http://localhost:8080/hook"},
		{name: "http loopback", text: "http://127.0.0.1/hook"},
		{name: "2048 chars", text: base + strings.Repeat("a", webhook.MaximumURLLength-len(base))},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := webhook.ParseEndpointURL(testCase.text)
			if err != nil || string(got) != testCase.text {
				t.Fatalf("got (%q, %v)", got, err)
			}
		})
	}
}

func TestParseEndpointURL_RejectsOtherTargets(t *testing.T) {
	base := "https://example.com/"
	cases := []struct {
		name string
		text string
	}{
		{name: "http other host", text: "http://example.com"},
		{name: "relative", text: "/hook"},
		{name: "ftp", text: "ftp://example.com"},
		{name: "empty", text: ""},
		{name: "https without host", text: "https://"},
		{name: "unparsable", text: "https://[::1"},
		{name: "2049 chars", text: base + strings.Repeat("a", webhook.MaximumURLLength-len(base)+1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := webhook.ParseEndpointURL(testCase.text); !errors.Is(err, webhook.ErrInvalidEndpointURL) {
				t.Fatalf("error = %v, want ErrInvalidEndpointURL", err)
			}
		})
	}
}

func TestNewEndpoint_Valid_IsActive(t *testing.T) {
	created, err := webhook.NewEndpoint("e1", "o1", "https://example.com", validSecret, createdAt)
	if err != nil {
		t.Fatalf("NewEndpoint: %v", err)
	}
	if created.ID() != "e1" || created.OwnerID() != "o1" || created.URL() != "https://example.com" {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Secret() != validSecret || !created.IsActive() || !created.CreatedAt().Equal(createdAt) {
		t.Fatalf("unexpected state: %+v", created)
	}
}

func TestNewEndpoint_BadSecret_ReturnsErrInvalidSecret(t *testing.T) {
	_, err := webhook.NewEndpoint("e1", "o1", "https://example.com", "whsec_short", createdAt)
	if !errors.Is(err, webhook.ErrInvalidSecret) {
		t.Fatalf("error = %v, want ErrInvalidSecret", err)
	}
}

func TestDisable_Twice_ReturnsErrEndpointAlreadyDisabled(t *testing.T) {
	target, _ := webhook.NewEndpoint("e1", "o1", "https://example.com", validSecret, createdAt)
	if err := target.Disable(); err != nil || target.IsActive() {
		t.Fatalf("first disable: active %v, error %v", target.IsActive(), err)
	}
	if err := target.Disable(); !errors.Is(err, webhook.ErrEndpointAlreadyDisabled) {
		t.Fatalf("error = %v, want ErrEndpointAlreadyDisabled", err)
	}
}
