package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, url string, maximumConnections int32) (*pgxpool.Pool, error) {
	return NewTracedPool(ctx, url, maximumConnections, nil)
}

func NewTracedPool(ctx context.Context, url string, maximumConnections int32, spans QuerySpans) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	config.MaxConns = maximumConnections
	if spans != nil {
		config.ConnConfig.Tracer = queryTracer{spans: spans}
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("creating database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return pool, nil
}
