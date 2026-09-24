//go:build integration

package postgres_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

var allMigrations = []string{
	"0001_accounts.sql",
	"0002_ledger.sql",
	"0003_transfers.sql",
	"0004_api_keys.sql",
	"0005_webhooks.sql",
	"0006_payments.sql",
}

func TestNewPool_InvalidInput_ReturnsError(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		maximumConnections int32
	}{
		{name: "bad url", url: "://bad", maximumConnections: 1},
		{name: "unreachable host", url: "postgres://paygo@127.0.0.1:1/paygo", maximumConnections: 1},
		{name: "no connections", url: adminURL(), maximumConnections: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := postgres.NewPool(ctx, test.url, test.maximumConnections); err == nil {
				t.Fatal("NewPool() error = nil, want an error")
			}
		})
	}
}

func TestUp_FreshDatabase_AppliesEveryFile(t *testing.T) {
	applied, err := postgres.NewMigrator(newEmptyTestPool(t)).Migrate(context.Background())
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if !slices.Equal(applied, allMigrations) {
		t.Fatalf("applied = %v, want %v", applied, allMigrations)
	}
}

func TestUp_AlreadyMigrated_AppliesNothing(t *testing.T) {
	applied, err := postgres.NewMigrator(newTestPool(t)).Migrate(context.Background())
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v, want none", applied)
	}
}

func TestUp_Concurrent_AppliesEachFileOnce(t *testing.T) {
	migrator := postgres.NewMigrator(newEmptyTestPool(t))
	results := make([][]string, 2)
	failures := make([]error, 2)
	var running sync.WaitGroup
	for index := range results {
		running.Go(func() {
			results[index], failures[index] = migrator.Migrate(context.Background())
		})
	}
	running.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatalf("Up() error = %v", err)
		}
	}
	total := slices.Concat(results[0], results[1])
	slices.Sort(total)
	if !slices.Equal(total, allMigrations) {
		t.Fatalf("applied = %v, want %v", total, allMigrations)
	}
}

func TestUp_ClosedPool_ReturnsError(t *testing.T) {
	pool := newEmptyTestPool(t)
	pool.Close()
	if _, err := postgres.NewMigrator(pool).Migrate(context.Background()); err == nil {
		t.Fatal("Up() error = nil, want an error")
	}
}

func TestUp_MigrationFails_ReturnsErrorAndKeepsItUnrecorded(t *testing.T) {
	pool := newEmptyTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE TABLE accounts (id uuid)"); err != nil {
		t.Fatalf("preparing conflict: %v", err)
	}

	applied, err := postgres.NewMigrator(pool).Migrate(ctx)

	if err == nil || len(applied) != 0 {
		t.Fatalf("Up() = %v, %v, want nothing applied and an error", applied, err)
	}
	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil {
		t.Fatalf("counting migrations: %v", err)
	}
	if recorded != 0 {
		t.Fatalf("recorded = %d, want 0", recorded)
	}
}

func TestUp_MigrationsRelationIsReadOnly_ReturnsError(t *testing.T) {
	pool := newEmptyTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE VIEW schema_migrations AS SELECT 'x'::text AS version"); err != nil {
		t.Fatalf("preparing view: %v", err)
	}
	if _, err := postgres.NewMigrator(pool).Migrate(ctx); err == nil {
		t.Fatal("Up() error = nil, want an error")
	}
}

func TestUp_Migrated_SeedsSettlementAccounts(t *testing.T) {
	rows, err := newTestPool(t).Query(context.Background(),
		"SELECT id::text FROM accounts WHERE kind = 'settlement' ORDER BY id")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		got = append(got, id)
	}
	want := []string{
		"01900000-0000-7000-8000-000000000001",
		"01900000-0000-7000-8000-000000000002",
		"01900000-0000-7000-8000-000000000003",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("settlement ids = %v, want %v", got, want)
	}
}

func TestLedgerEntries_Change_IsRejected(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	const insert = `INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount_minor, currency, created_at)
		VALUES ('01900000-0000-7000-8000-0000000000aa', '01900000-0000-7000-8000-0000000000bb',
		'01900000-0000-7000-8000-000000000001', 'debit', 1, 'BRL', now())`
	if _, err := pool.Exec(ctx, insert); err != nil {
		t.Fatalf("inserting entry: %v", err)
	}
	for _, statement := range []string{"UPDATE ledger_entries SET amount_minor = 2", "DELETE FROM ledger_entries"} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatalf("%q succeeded, want the trigger error", statement)
		}
	}
}
