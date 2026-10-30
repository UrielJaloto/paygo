//go:build integration

package main

import (
	"os"
	"testing"
)

const defaultAdminURL = "postgres://paygo:paygo@localhost:5432/postgres?sslmode=disable"

func TestRun_PostgresStorage_ServesUntilCancelled(t *testing.T) {
	databaseURL := os.Getenv("PAYGO_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = defaultAdminURL
	}
	env := baseEnv(freeAddress(t))
	env["PAYGO_DATABASE_URL"] = databaseURL
	worker := startWorker(t, env)
	worker.waitHealthy(t)
	worker.cancel()
	if code := worker.exitCode(t); code != 0 {
		t.Errorf("exit = %d, stderr %q", code, worker.stderr.String())
	}
}
