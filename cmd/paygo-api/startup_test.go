package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestRun_MemoryStorage_IsAlwaysReady(t *testing.T) {
	env := memoryEnv(freeAddress(t))
	running := startAPI(t, env)
	running.waitHealthy(t)
	response, err := http.Get("http://" + env["PAYGO_HTTP_ADDR"] + "/readyz")
	if err != nil {
		t.Fatalf("requesting readiness: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestRun_TracingFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := memoryEnv("127.0.0.1:0")
	env["PAYGO_OTLP_ENDPOINT"] = "127.0.0.1:1"
	var stderr bytes.Buffer
	code := run(ctx, buildLookup(env), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Errorf("exit = %d, stderr %q", code, stderr.String())
	}
}
