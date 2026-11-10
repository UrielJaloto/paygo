package webhookhttp_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/webhookhttp"
)

func TestNewClient_InternalDestination_IsRefused(t *testing.T) {
	client := webhookhttp.NewClient(time.Second)
	targets := []string{
		"http://10.0.0.1:9/",
		"http://172.17.0.2:9/",
		"http://192.168.1.1:9/",
		"http://169.254.169.254:9/latest/meta-data",
		"http://100.64.0.1:9/",
		"http://[fd00::1]:9/",
		"http://[::ffff:10.0.0.1]:9/",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			_, err := client.Get(target)
			if !errors.Is(err, webhookhttp.ErrForbiddenDestination) {
				t.Fatalf("err = %v, want ErrForbiddenDestination", err)
			}
		})
	}
}

func TestNewClient_LoopbackDestination_IsAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	response, err := webhookhttp.NewClient(time.Second).Get(server.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
}

func TestNewClient_Redirect_IsNotFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()
	response, err := webhookhttp.NewClient(time.Second).Get(server.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusFound)
	}
}
