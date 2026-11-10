//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_SmokeAgainstPaygo_IsCorrect(t *testing.T) {
	url := os.Getenv("PAYGO_BENCH_API_URL")
	if url == "" {
		t.Fatal("PAYGO_BENCH_API_URL is required")
	}
	arguments := []string{"-accounts", "20", "-transfers", "1000", "-clients", "8", "-api-url", url, "-database-url", os.Getenv("PAYGO_BENCH_DATABASE_URL"), "-out", filepath.Join(t.TempDir(), "report.json")}
	var output bytes.Buffer
	if err := run(context.Background(), arguments, os.LookupEnv, &output); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output.String(), "errors 0.0000, correct true") {
		t.Errorf("output = %q", output.String())
	}
}
