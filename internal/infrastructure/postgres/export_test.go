package postgres

import (
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	RunInTransaction  = runInTransaction
	IsUniqueViolation = isUniqueViolation
	NotFound          = notFound
)

func NewMigratorFrom(pool *pgxpool.Pool, files fs.FS) *Migrator {
	return &Migrator{pool: pool, files: files}
}

func NewQueryTracer(spans QuerySpans) queryTracer {
	return queryTracer{spans: spans}
}
