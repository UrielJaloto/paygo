package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	paymentservice "github.com/UrielJaloto/paygo/internal/services/payment"
)

type PaymentUnitOfWork struct {
	pool *pgxpool.Pool
}

func NewPaymentUnitOfWork(pool *pgxpool.Pool) *PaymentUnitOfWork {
	return &PaymentUnitOfWork{pool: pool}
}

func (u *PaymentUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store paymentservice.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, transaction pgx.Tx) error {
		return work(ctx, paymentservice.Store{
			Accounts:  &AccountRepository{database: transaction},
			Ledger:    &LedgerRepository{database: transaction},
			Transfers: &TransferRepository{database: transaction},
			Charges:   &ChargeRepository{database: transaction},
			Payouts:   &PayoutRepository{database: transaction},
			Events:    &ProviderEventLog{database: transaction},
			Outbox:    &OutboxRepository{database: transaction},
		})
	})
}
