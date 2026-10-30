//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

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
