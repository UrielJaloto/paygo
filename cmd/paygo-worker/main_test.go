//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/webhook"
	"github.com/UrielJaloto/paygo/internal/infrastructure/config"
)

const (
	bootDeadline        = 10 * time.Second
	testToken           = "0123456789abcdef0123456789abcdef"
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

func unreachableEnvironment(address string) map[string]string {
	environment := baseEnvironment(address)
	environment["PAYGO_DATABASE_URL"] = unreachableDatabase
	return environment
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

func buildLookup(environment map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := environment[name]
		return value, found
	}
}

func startWorker(t *testing.T, environment map[string]string) runningWorker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	worker := runningWorker{address: environment["PAYGO_HTTP_ADDR"], cancel: cancel, done: make(chan int, 1), stderr: &bytes.Buffer{}}
	go func() {
		worker.done <- run(ctx, buildLookup(environment), &bytes.Buffer{}, worker.stderr)
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

func TestRun_StartupFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	missingToken := unreachableEnvironment("127.0.0.1:0")
	delete(missingToken, "PAYGO_FAKE_WEBHOOK_TOKEN")
	tests := []struct {
		name        string
		environment map[string]string
		want        string
	}{
		{name: "invalid config", environment: missingToken, want: "PAYGO_FAKE_WEBHOOK_TOKEN"},
		{name: "unreachable database", environment: unreachableEnvironment("127.0.0.1:0"), want: "pinging database"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := run(context.Background(), buildLookup(testCase.environment), &bytes.Buffer{}, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), testCase.want) {
				t.Errorf("exit = %d, stderr %q, want %q", code, stderr.String(), testCase.want)
			}
		})
	}
}

func TestNewDeliverInput_ValidAttempts_AddsTheLeaseMargin(t *testing.T) {
	input, err := newDeliverInput(config.Worker{Concurrency: 4, WebhookTimeout: 5 * time.Second, WebhookMaximumAttempts: 3})
	if err != nil {
		t.Fatalf("newDeliverInput: %v", err)
	}
	if input.Limit != 4 || input.Lease != 35*time.Second {
		t.Errorf("input = %+v", input)
	}
}

func TestNewDeliverInput_InvalidAttempts_ReturnsPolicyError(t *testing.T) {
	_, err := newDeliverInput(config.Worker{WebhookMaximumAttempts: 0})
	if !errors.Is(err, webhook.ErrInvalidRetryPolicy) {
		t.Errorf("err = %v, want ErrInvalidRetryPolicy", err)
	}
}

func TestRun_TracingFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	environment := unreachableEnvironment("127.0.0.1:0")
	environment["PAYGO_OTLP_ENDPOINT"] = "127.0.0.1:1"
	var stderr bytes.Buffer
	code := run(ctx, buildLookup(environment), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Errorf("exit = %d, stderr %q", code, stderr.String())
	}
}

func TestRunPollers_InvalidAttempts_ReturnsPolicyError(t *testing.T) {
	err := runPollers(context.Background(), config.WorkerSettings{}, toolkit{})
	if !errors.Is(err, webhook.ErrInvalidRetryPolicy) {
		t.Errorf("err = %v, want ErrInvalidRetryPolicy", err)
	}
}

const defaultAdminURL = "postgres://paygo:example-postgres-password@localhost:5432/postgres?sslmode=disable"

func postgresEnvironment(address string) map[string]string {
	environment := baseEnvironment(address)
	environment["PAYGO_DATABASE_URL"] = defaultAdminURL
	if databaseURL := os.Getenv("PAYGO_TEST_DATABASE_URL"); databaseURL != "" {
		environment["PAYGO_DATABASE_URL"] = databaseURL
	}
	return environment
}

func TestRun_Postgres_ServesUntilCancelled(t *testing.T) {
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
			environment := postgresEnvironment(freeAddress(t))
			for name, value := range testCase.extra {
				environment[name] = value
			}
			worker := startWorker(t, environment)
			worker.waitHealthy(t)
			worker.cancel()
			if code := worker.exitCode(t); code != 0 {
				t.Errorf("exit = %d, stderr %q", code, worker.stderr.String())
			}
		})
	}
}

func TestRun_OccupiedPort_ReturnsOneAndNamesTheCause(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer occupied.Close()
	var stderr bytes.Buffer
	code := run(context.Background(), buildLookup(postgresEnvironment(occupied.Addr().String())), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "listening on") {
		t.Errorf("exit = %d, stderr %q", code, stderr.String())
	}
}
