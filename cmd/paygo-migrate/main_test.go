//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type outcome struct {
	code   int
	stdout string
	stderr string
}

func runWith(env map[string]string) outcome {
	var stdout, stderr bytes.Buffer
	lookup := func(name string) (string, bool) {
		value, found := env[name]
		return value, found
	}
	code := run(context.Background(), lookup, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func appliedFiles(t *testing.T, output string) []string {
	t.Helper()
	var entry struct {
		Message   string   `json:"msg"`
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("decoding log %q: %v", output, err)
	}
	if entry.Message != "migrations applied" {
		t.Errorf("msg = %q", entry.Message)
	}
	return entry.Files
}

func TestRun_FreshDatabaseThenSecondRun_AppliesOnce(t *testing.T) {
	env := map[string]string{"PAYGO_DATABASE_URL": newDatabaseURL(t)}
	first := runWith(env)
	if first.code != 0 {
		t.Fatalf("first run = %d, stderr %q", first.code, first.stderr)
	}
	if files := appliedFiles(t, first.stdout); len(files) != 6 {
		t.Errorf("first run applied %v, want 6 files", files)
	}
	second := runWith(env)
	if second.code != 0 {
		t.Fatalf("second run = %d, stderr %q", second.code, second.stderr)
	}
	if files := appliedFiles(t, second.stdout); len(files) != 0 {
		t.Errorf("second run applied %v, want none", files)
	}
}

func TestRun_Failure_ReturnsOneAndReportsOnStderr(t *testing.T) {
	broken := newDatabaseURL(t)
	execute(t, broken, "CREATE TABLE schema_migrations (id integer)")
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "missing url", env: map[string]string{}, want: "PAYGO_DATABASE_URL"},
		{name: "database down", env: map[string]string{"PAYGO_DATABASE_URL": "postgres://paygo:paygo@127.0.0.1:1/paygo?connect_timeout=1"}, want: "pinging database"},
		{name: "migration fails", env: map[string]string{"PAYGO_DATABASE_URL": broken}, want: "applying migration"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := runWith(testCase.env)
			if got.code != 1 {
				t.Errorf("code = %d, want 1", got.code)
			}
			if !strings.HasPrefix(got.stderr, "paygo-migrate: ") || !strings.Contains(got.stderr, testCase.want) {
				t.Errorf("stderr = %q, want it to mention %q", got.stderr, testCase.want)
			}
		})
	}
}
