package postgres

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	createMigrationsTable = `
		SELECT pg_advisory_xact_lock(727274);
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`
	claimMigration = `
		WITH migration_lock AS (SELECT pg_advisory_xact_lock(727274))
		INSERT INTO schema_migrations (version)
		SELECT $1 FROM migration_lock
		ON CONFLICT DO NOTHING`
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Migrator struct {
	pool *pgxpool.Pool
}

func NewMigrator(pool *pgxpool.Pool) *Migrator {
	return &Migrator{pool: pool}
}

func (m *Migrator) Migrate(ctx context.Context) ([]string, error) {
	if _, err := m.pool.Exec(ctx, createMigrationsTable); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}
	entries, _ := migrationFiles.ReadDir("migrations")
	applied := []string{}
	for _, entry := range entries {
		wasApplied, err := m.applyMigration(ctx, entry.Name())
		if err != nil {
			return applied, fmt.Errorf("applying migration %s: %w", entry.Name(), err)
		}
		if wasApplied {
			applied = append(applied, entry.Name())
		}
	}
	return applied, nil
}

func (m *Migrator) applyMigration(ctx context.Context, name string) (bool, error) {
	wasApplied := false
	err := pgx.BeginFunc(ctx, m.pool, func(transaction pgx.Tx) error {
		claim, err := transaction.Exec(ctx, claimMigration, name)
		if err != nil {
			return err
		}
		if claim.RowsAffected() == 0 {
			return nil
		}
		content, _ := migrationFiles.ReadFile("migrations/" + name)
		wasApplied = true
		_, err = transaction.Exec(ctx, string(content))
		return err
	})
	return wasApplied, err
}
