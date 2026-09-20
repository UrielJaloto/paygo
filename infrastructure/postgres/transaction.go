package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maximumTransactionAttempts = 4
	serializationFailure   = "40001"
	deadlockDetected       = "40P01"
)

type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

func runInTransaction(ctx context.Context, pool *pgxpool.Pool, work func(ctx context.Context, transaction pgx.Tx) error) error {
	var err error
	for range maximumTransactionAttempts {
		err = attemptTransaction(ctx, pool, work)
		if !isRetryable(err) {
			return err
		}
	}
	return err
}

func attemptTransaction(ctx context.Context, pool *pgxpool.Pool, work func(ctx context.Context, transaction pgx.Tx) error) error {
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := work(ctx, transaction); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func isRetryable(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	return postgresError.Code == serializationFailure || postgresError.Code == deadlockDetected
}
