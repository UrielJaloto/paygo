//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
)

const (
	defaultAdminURL = "postgres://paygo:paygo@localhost:5432/postgres?sslmode=disable"
	migrationsDirectory   = "../../../env/migrations"
)

func migrationFiles() fs.FS {
	return os.DirFS(migrationsDirectory)
}

func adminURL() string {
	if configured := os.Getenv("PAYGO_TEST_DATABASE_URL"); configured != "" {
		return configured
	}
	return defaultAdminURL
}

func newEmptyTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL())
	if err != nil {
		t.Fatalf("connecting to the admin database: %v", err)
	}
	defer admin.Close(ctx)
	name := "paygo_test_" + hex.EncodeToString(randomBytes(t))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating test database: %v", err)
	}
	parsed, err := url.Parse(adminURL())
	if err != nil {
		t.Fatalf("parsing the admin url: %v", err)
	}
	parsed.Path = "/" + name
	pool, err := postgres.NewPool(ctx, parsed.String(), 10)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	t.Cleanup(func() { dropDatabase(t, pool, name) })
	return pool
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newEmptyTestPool(t)
	if _, err := postgres.NewMigrator(pool, migrationFiles()).Migrate(context.Background()); err != nil {
		t.Fatalf("migrating the test database: %v", err)
	}
	return pool
}

func randomBytes(t *testing.T) []byte {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("reading random bytes: %v", err)
	}
	return random
}

func dropDatabase(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()
	pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL())
	if err != nil {
		t.Errorf("connecting to the admin database: %v", err)
		return
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
		t.Errorf("dropping test database: %v", err)
	}
}
