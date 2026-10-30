package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	webhookservice "github.com/UrielJaloto/paygo/internal/services/webhook"
)

type WebhookUnitOfWork struct {
	pool *pgxpool.Pool
}

func NewWebhookUnitOfWork(pool *pgxpool.Pool) *WebhookUnitOfWork {
	return &WebhookUnitOfWork{pool: pool}
}

func (u *WebhookUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store webhookservice.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, transaction pgx.Tx) error {
		return work(ctx, webhookservice.Store{
			Endpoints:  &EndpointRepository{database: transaction},
			Deliveries: &DeliveryRepository{database: transaction},
		})
	})
}
