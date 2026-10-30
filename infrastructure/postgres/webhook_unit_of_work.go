package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
)

type WebhookUnitOfWork struct {
	pool *pgxpool.Pool
}

func NewWebhookUnitOfWork(pool *pgxpool.Pool) *WebhookUnitOfWork {
	return &WebhookUnitOfWork{pool: pool}
}

func (u *WebhookUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store webhookservice.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, tx pgx.Tx) error {
		return work(ctx, webhookservice.Store{
			Endpoints:  &EndpointRepository{db: tx},
			Deliveries: &DeliveryRepository{db: tx},
		})
	})
}
