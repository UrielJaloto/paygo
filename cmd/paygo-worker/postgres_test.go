//go:build integration

package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"strings"
	"testing"
)

const defaultAdminURL = "postgres://paygo:paygo@localhost:5432/postgres?sslmode=disable"

func postgresEnv(address string) map[string]string {
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
			env := postgresEnv(freeAddress(t))
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

func TestRun_OccupiedPort_ReturnsOneAndNamesTheCause(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer occupied.Close()
	var stderr bytes.Buffer
	code := run(context.Background(), buildLookup(postgresEnv(occupied.Addr().String())), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "listening on") {
		t.Errorf("exit = %d, stderr %q", code, stderr.String())
	}
}
