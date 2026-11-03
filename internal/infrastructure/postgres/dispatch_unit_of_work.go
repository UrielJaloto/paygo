package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/services/dispatch"
)

type DispatchUnitOfWork struct {
	pool *pgxpool.Pool
	ids  dispatch.IDGenerator
}

func NewDispatchUnitOfWork(pool *pgxpool.Pool, ids dispatch.IDGenerator) *DispatchUnitOfWork {
	return &DispatchUnitOfWork{pool: pool, ids: ids}
}

func (u *DispatchUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store dispatch.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, tx pgx.Tx) error {
		return work(ctx, dispatch.Store{
			Outbox:     &OutboxRepository{db: tx},
			Endpoints:  &EndpointRepository{db: tx},
			Deliveries: &DeliveryRepository{db: tx},
			IDs:        u.ids,
		})
	})
}
