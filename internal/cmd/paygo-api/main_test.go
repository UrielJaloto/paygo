//go:build integration

package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	bootDeadline  = 10 * time.Second
	testToken     = "0123456789abcdef0123456789abcdef"
	unreachableDatabase = "postgres://paygo:paygo@127.0.0.1:1/paygo?sslmode=disable&connect_timeout=2"
)

type runningAPI struct {
	address string
	cancel  context.CancelFunc
	done    chan int
	stderr  *bytes.Buffer
}

func baseEnvironment(address string) map[string]string {
	return map[string]string{
		"PAYGO_HTTP_ADDR":          address,
		"PAYGO_ADMIN_TOKEN":        testToken,
		"PAYGO_FAKE_WEBHOOK_TOKEN": testToken,
	}
}

func unreachableEnvironment(address string) map[string]string {
	env := baseEnvironment(address)
	env["PAYGO_DATABASE_URL"] = unreachableDatabase
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

func startAPI(t *testing.T, env map[string]string) runningAPI {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	api := runningAPI{address: env["PAYGO_HTTP_ADDR"], cancel: cancel, done: make(chan int, 1), stderr: &bytes.Buffer{}}
	go func() {
		api.done <- run(ctx, buildLookup(env), &bytes.Buffer{}, api.stderr)
	}()
	return api
}

func (a runningAPI) waitHealthy(t *testing.T) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(bootDeadline)
	for !a.isHealthy() {
		select {
		case code := <-a.done:
			t.Fatalf("exited with %d before healthy, stderr %q", code, a.stderr.String())
		case <-deadline:
			t.Fatal("not healthy within the deadline")
		case <-ticker.C:
		}
	}
}

func (a runningAPI) isHealthy() bool {
	response, err := http.Get("http://" + a.address + "/healthz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (a runningAPI) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-a.done:
		return code
	case <-time.After(bootDeadline):
		t.Fatal("did not exit within the deadline")
		return -1
	}
}

func TestRun_StartupFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	missingToken := unreachableEnvironment("127.0.0.1:0")
	delete(missingToken, "PAYGO_ADMIN_TOKEN")
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "invalid config", env: missingToken, want: "PAYGO_ADMIN_TOKEN"},
		{name: "unreachable database", env: unreachableEnvironment("127.0.0.1:0"), want: "pinging database"},
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

func TestRun_TracingFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := unreachableEnvironment("127.0.0.1:0")
	env["PAYGO_OTLP_ENDPOINT"] = "127.0.0.1:1"
	var stderr bytes.Buffer
	code := run(ctx, buildLookup(env), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Errorf("exit = %d, stderr %q", code, stderr.String())
	}
}

const defaultAdminURL = "postgres://paygo:paygo@localhost:5432/postgres?sslmode=disable"

func postgresEnvironment(address string) map[string]string {
	env := baseEnvironment(address)
	env["PAYGO_DATABASE_URL"] = defaultAdminURL
	if databaseURL := os.Getenv("PAYGO_TEST_DATABASE_URL"); databaseURL != "" {
		env["PAYGO_DATABASE_URL"] = databaseURL
	}
	return env
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
			env := postgresEnvironment(freeAddress(t))
			for name, value := range testCase.extra {
				env[name] = value
			}
			api := startAPI(t, env)
			api.waitHealthy(t)
			api.cancel()
			if code := api.exitCode(t); code != 0 {
				t.Errorf("exit = %d, stderr %q", code, api.stderr.String())
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

func TestRun_Postgres_IsReady(t *testing.T) {
	env := postgresEnvironment(freeAddress(t))
	api := startAPI(t, env)
	api.waitHealthy(t)
	response, err := http.Get("http://" + env["PAYGO_HTTP_ADDR"] + "/readyz")
	if err != nil {
		t.Fatalf("requesting readiness: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}
