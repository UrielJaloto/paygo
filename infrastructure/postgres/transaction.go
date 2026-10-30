package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxTransactionAttempts = 4
	serializationFailure   = "40001"
	deadlockDetected       = "40P01"
)

type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

func runInTransaction(ctx context.Context, pool *pgxpool.Pool, work func(ctx context.Context, tx pgx.Tx) error) error {
	var err error
	for range maxTransactionAttempts {
		err = attemptTransaction(ctx, pool, work)
		if !isRetryable(err) {
			return err
		}
	}
	return err
}

func attemptTransaction(ctx context.Context, pool *pgxpool.Pool, work func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := work(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == serializationFailure || pgErr.Code == deadlockDetected
}
