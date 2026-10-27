package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/config"
)

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

func TestWorkTraced_InvalidAttempts_ReturnsPolicyError(t *testing.T) {
	err := workTraced(context.Background(), config.WorkerSettings{}, toolkit{})
	if !errors.Is(err, webhook.ErrInvalidRetryPolicy) {
		t.Errorf("err = %v, want ErrInvalidRetryPolicy", err)
	}
}
