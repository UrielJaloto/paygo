package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type walProbe struct {
	connection *pgx.Conn
	start      string
}

func openWALProbe(ctx context.Context, databaseURL string) (*walProbe, string, error) {
	if databaseURL == "" {
		return nil, "", nil
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, "", fmt.Errorf("connecting to postgres: %w", err)
	}
	var version string
	if err := connection.QueryRow(ctx, "SHOW server_version").Scan(&version); err != nil {
		return nil, "", fmt.Errorf("reading postgres version: %w", err)
	}
	return &walProbe{connection: connection}, version, nil
}

func (w *walProbe) markPosition(ctx context.Context) error {
	if w == nil {
		return nil
	}
	return w.connection.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&w.start)
}

func (w *walProbe) measureWALSinceMark(ctx context.Context) (int64, error) {
	if w == nil {
		return 0, nil
	}
	var written int64
	query := "SELECT pg_current_wal_lsn() - $1::text::pg_lsn"
	if err := w.connection.QueryRow(ctx, query, w.start).Scan(&written); err != nil {
		return 0, fmt.Errorf("measuring wal: %w", err)
	}
	return written, nil
}

func (w *walProbe) close(ctx context.Context) {
	if w != nil {
		_ = w.connection.Close(ctx)
	}
}
