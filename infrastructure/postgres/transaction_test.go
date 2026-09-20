//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

func newProbePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newEmptyTestPool(t)
	if _, err := pool.Exec(context.Background(), "CREATE TABLE probe (id int)"); err != nil {
		t.Fatalf("creating probe table: %v", err)
	}
	return pool
}

func insertProbe(ctx context.Context, transaction pgx.Tx) error {
	_, err := transaction.Exec(ctx, "INSERT INTO probe VALUES (1)")
	return err
}

func probeRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var rows int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM probe").Scan(&rows); err != nil {
		t.Fatalf("counting probes: %v", err)
	}
	return rows
}

func TestRunInTransaction_WorkSucceeds_Commits(t *testing.T) {
	pool := newProbePool(t)
	if err := postgres.RunInTransaction(context.Background(), pool, insertProbe); err != nil {
		t.Fatalf("RunInTransaction() error = %v", err)
	}
	if rows := probeRows(t, pool); rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
}

func TestRunInTransaction_WorkFails_RollsBackAndReturnsError(t *testing.T) {
	pool := newProbePool(t)
	err := postgres.RunInTransaction(context.Background(), pool, func(ctx context.Context, transaction pgx.Tx) error {
		if err := insertProbe(ctx, transaction); err != nil {
			t.Fatalf("inserting probe: %v", err)
		}
		return errDomain
	})
	if !errors.Is(err, errDomain) {
		t.Fatalf("RunInTransaction() error = %v, want %v", err, errDomain)
	}
	if rows := probeRows(t, pool); rows != 0 {
		t.Fatalf("rows = %d, want 0", rows)
	}
}

func TestRunInTransaction_WorkPanics_RollsBackAndPropagates(t *testing.T) {
	pool := newProbePool(t)
	defer func() {
		if recovered := recover(); recovered != "boom" {
			t.Fatalf("recovered = %v, want boom", recovered)
		}
		if rows := probeRows(t, pool); rows != 0 {
			t.Fatalf("rows = %d, want 0", rows)
		}
	}()
	_ = postgres.RunInTransaction(context.Background(), pool, func(ctx context.Context, transaction pgx.Tx) error {
		if err := insertProbe(ctx, transaction); err != nil {
			t.Fatalf("inserting probe: %v", err)
		}
		panic("boom")
	})
}

func TestRunInTransaction_TransientFailures_Retries(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		failures     int
		wantAttempts int
		wantRows     int
		wantErr      bool
	}{
		{name: "serialization then success", code: "40001", failures: 2, wantAttempts: 3, wantRows: 1},
		{name: "deadlock then success", code: "40P01", failures: 2, wantAttempts: 3, wantRows: 1},
		{name: "gives up after three retries", code: "40001", failures: 10, wantAttempts: 4, wantErr: true},
		{name: "other code is not retried", code: "23505", failures: 10, wantAttempts: 1, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := newProbePool(t)
			attempts := 0
			err := postgres.RunInTransaction(context.Background(), pool, func(ctx context.Context, transaction pgx.Tx) error {
				attempts++
				if attempts <= test.failures {
					return &pgconn.PgError{Code: test.code}
				}
				return insertProbe(ctx, transaction)
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("RunInTransaction() error = %v, wantErr %v", err, test.wantErr)
			}
			if attempts != test.wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, test.wantAttempts)
			}
			if rows := probeRows(t, pool); rows != test.wantRows {
				t.Fatalf("rows = %d, want %d", rows, test.wantRows)
			}
		})
	}
}

func TestRunInTransaction_ClosedPool_ReturnsError(t *testing.T) {
	pool := newEmptyTestPool(t)
	pool.Close()
	err := postgres.RunInTransaction(context.Background(), pool, insertProbe)
	if err == nil {
		t.Fatal("RunInTransaction() error = nil, want an error")
	}
}

func TestIsReady_PoolState_ReportsReadiness(t *testing.T) {
	pool := newEmptyTestPool(t)
	checker := postgres.NewHealthChecker(pool)
	if !checker.IsReady(context.Background()) {
		t.Fatal("IsReady() = false on a live pool, want true")
	}
	pool.Close()
	if checker.IsReady(context.Background()) {
		t.Fatal("IsReady() = true on a closed pool, want false")
	}
}
