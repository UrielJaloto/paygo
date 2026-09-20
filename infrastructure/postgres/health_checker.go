package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const readinessTimeout = time.Second

type HealthChecker struct {
	pool *pgxpool.Pool
}

func NewHealthChecker(pool *pgxpool.Pool) *HealthChecker {
	return &HealthChecker{pool: pool}
}

func (c *HealthChecker) IsReady(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()
	_, err := c.pool.Exec(ctx, "SELECT 1")
	return err == nil
}
