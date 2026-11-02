//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
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

const defaultAdminURL = "postgres://paygo:paygo@localhost:5432/postgres?sslmode=disable"

func adminURL() string {
	if configured := os.Getenv("PAYGO_TEST_DATABASE_URL"); configured != "" {
		return configured
	}
	return defaultAdminURL
}

func newDatabaseURL(t *testing.T) string {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("reading random bytes: %v", err)
	}
	name := "paygo_test_" + hex.EncodeToString(random)
	execute(t, adminURL(), "CREATE DATABASE "+name)
	t.Cleanup(func() { execute(t, adminURL(), "DROP DATABASE "+name+" WITH (FORCE)") })
	parsed, err := url.Parse(adminURL())
	if err != nil {
		t.Fatalf("parsing the admin url: %v", err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func execute(t *testing.T, databaseURL, statement string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.NewPool(ctx, databaseURL, 2)
	if err != nil {
		t.Fatalf("connecting to the database: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("executing %q: %v", statement, err)
	}
}
