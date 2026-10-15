package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/config"
)

const (
	bootDeadline  = 10 * time.Second
	testToken     = "0123456789abcdef0123456789abcdef"
	unreachableDatabase = "postgres://paygo:paygo@127.0.0.1:1/paygo?sslmode=disable&connect_timeout=2"
)

type runningWorker struct {
	address string
	cancel  context.CancelFunc
	done    chan int
	stderr  *bytes.Buffer
}

func baseEnvironment(address string) map[string]string {
	return map[string]string{
		"PAYGO_HTTP_ADDR":          address,
		"PAYGO_FAKE_WEBHOOK_TOKEN": testToken,
		"PAYGO_SHUTDOWN_TIMEOUT":   "2s",
	}
}

func memoryEnv(address string) map[string]string {
	env := baseEnvironment(address)
	env["PAYGO_STORAGE"] = "memory"
	return env
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func buildLookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := env[name]
		return value, found
	}
}

func startWorker(t *testing.T, env map[string]string) runningWorker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	worker := runningWorker{address: env["PAYGO_HTTP_ADDR"], cancel: cancel, done: make(chan int, 1), stderr: &bytes.Buffer{}}
	go func() {
		worker.done <- run(ctx, buildLookup(env), &bytes.Buffer{}, worker.stderr)
	}()
	return worker
}

func (w runningWorker) waitHealthy(t *testing.T) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(bootDeadline)
	for !w.isHealthy() {
		select {
		case code := <-w.done:
			t.Fatalf("exited with %d before healthy, stderr %q", code, w.stderr.String())
		case <-deadline:
			t.Fatal("not healthy within the deadline")
		case <-ticker.C:
		}
	}
}

func (w runningWorker) isHealthy() bool {
	response, err := http.Get("http://" + w.address + "/healthz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (w runningWorker) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-w.done:
		return code
	case <-time.After(bootDeadline):
		t.Fatal("did not exit within the deadline")
		return -1
	}
}

func TestRun_MemoryStorage_ServesUntilCancelled(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]string
	}{
		{name: "fake provider"},
		{name: "asaas provider", extra: map[string]string{
			"PAYGO_PROVIDER":            "asaas",
			"PAYGO_ASAAS_API_KEY":       "sandbox-key",
			"PAYGO_ASAAS_WEBHOOK_TOKEN": testToken,
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := memoryEnv(freeAddress(t))
			for name, value := range testCase.extra {
				env[name] = value
			}
			worker := startWorker(t, env)
			worker.waitHealthy(t)
			worker.cancel()
			if code := worker.exitCode(t); code != 0 {
				t.Errorf("exit = %d, stderr %q", code, worker.stderr.String())
			}
		})
	}
}

func TestRun_StartupFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer occupied.Close()
	unreachable := baseEnvironment("127.0.0.1:0")
	unreachable["PAYGO_DATABASE_URL"] = unreachableDatabase
	missingToken := memoryEnv("127.0.0.1:0")
	delete(missingToken, "PAYGO_FAKE_WEBHOOK_TOKEN")
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "invalid config", env: missingToken, want: "PAYGO_FAKE_WEBHOOK_TOKEN"},
		{name: "unreachable database", env: unreachable, want: "pinging database"},
		{name: "occupied port", env: memoryEnv(occupied.Addr().String()), want: "listening on"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := run(context.Background(), buildLookup(testCase.env), &bytes.Buffer{}, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), testCase.want) {
				t.Errorf("exit = %d, stderr %q, want %q", code, stderr.String(), testCase.want)
			}
		})
	}
}

func TestDeliverInput_ValidAttempts_AddsTheLeaseMargin(t *testing.T) {
	input, err := deliverInput(config.Worker{Concurrency: 4, WebhookTimeout: 5 * time.Second, WebhookMaximumAttempts: 3})
	if err != nil {
		t.Fatalf("deliverInput: %v", err)
	}
	if input.Limit != 4 || input.Lease != 35*time.Second {
		t.Errorf("input = %+v", input)
	}
}

func TestDeliverInput_InvalidAttempts_ReturnsPolicyError(t *testing.T) {
	_, err := deliverInput(config.Worker{WebhookMaximumAttempts: 0})
	if !errors.Is(err, webhook.ErrInvalidRetryPolicy) {
		t.Errorf("err = %v, want ErrInvalidRetryPolicy", err)
	}
}

func TestNewMux_Routes_ServeOnlyHealthAndMetrics(t *testing.T) {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	tests := []struct {
		path string
		want int
	}{
		{path: "/healthz", want: http.StatusOK},
		{path: "/metrics", want: http.StatusTeapot},
		{path: "/v1/accounts", want: http.StatusNotFound},
	}
	for _, testCase := range tests {
		t.Run(testCase.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			newMux(metrics).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, testCase.path, nil))
			if recorder.Code != testCase.want {
				t.Errorf("status = %d, want %d", recorder.Code, testCase.want)
			}
		})
	}
}
