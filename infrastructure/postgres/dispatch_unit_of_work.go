package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/services/dispatch"
)

type DispatchUnitOfWork struct {
	pool *pgxpool.Pool
	ids  dispatch.IDGenerator
}

func NewDispatchUnitOfWork(pool *pgxpool.Pool, ids dispatch.IDGenerator) *DispatchUnitOfWork {
	return &DispatchUnitOfWork{pool: pool, ids: ids}
}

func (u *DispatchUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store dispatch.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, transaction pgx.Tx) error {
		return work(ctx, dispatch.Store{
			Outbox:     &OutboxRepository{database: transaction},
			Endpoints:  &EndpointRepository{database: transaction},
			Deliveries: &DeliveryRepository{database: transaction},
			IDs:        u.ids,
		})
	})
}
