package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRun_TracingFailure_ReturnsOneAndNamesTheCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := unreachableEnv("127.0.0.1:0")
	env["PAYGO_OTLP_ENDPOINT"] = "127.0.0.1:1"
	var stderr bytes.Buffer
	code := run(ctx, lookupIn(env), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Errorf("exit = %d, stderr %q", code, stderr.String())
	}
}
