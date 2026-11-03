package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"path"

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

type Migrator struct {
	pool  *pgxpool.Pool
	files fs.FS
}

type migration struct {
	name    string
	content string
}

func NewMigrator(pool *pgxpool.Pool, files fs.FS) *Migrator {
	return &Migrator{pool: pool, files: files}
}

func (m *Migrator) Up(ctx context.Context) ([]string, error) {
	migrations, err := m.load()
	if err != nil {
		return nil, err
	}
	if _, err := m.pool.Exec(ctx, createMigrationsTable); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}
	applied := []string{}
	for _, next := range migrations {
		wasApplied, err := m.apply(ctx, next)
		if err != nil {
			return applied, fmt.Errorf("applying migration %s: %w", next.name, err)
		}
		if wasApplied {
			applied = append(applied, next.name)
		}
	}
	return applied, nil
}

func (m *Migrator) load() ([]migration, error) {
	entries, err := fs.ReadDir(m.files, ".")
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}
	migrations := []migration{}
	for _, entry := range entries {
		if path.Ext(entry.Name()) != ".sql" {
			continue
		}
		content, err := fs.ReadFile(m.files, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{name: entry.Name(), content: string(content)})
	}
	return migrations, nil
}

func (m *Migrator) apply(ctx context.Context, next migration) (bool, error) {
	wasApplied := false
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		claim, err := tx.Exec(ctx, claimMigration, next.name)
		if err != nil {
			return err
		}
		if claim.RowsAffected() == 0 {
			return nil
		}
		wasApplied = true
		_, err = tx.Exec(ctx, next.content)
		return err
	})
	return wasApplied, err
}
