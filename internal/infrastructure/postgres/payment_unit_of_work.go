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
	return runInTransaction(ctx, u.pool, func(ctx context.Context, tx pgx.Tx) error {
		return work(ctx, paymentservice.Store{
			Accounts:  &AccountRepository{db: tx},
			Ledger:    &LedgerRepository{db: tx},
			Transfers: &TransferRepository{db: tx},
			Charges:   &ChargeRepository{db: tx},
			Payouts:   &PayoutRepository{db: tx},
			Events:    &ProviderEventLog{db: tx},
			Outbox:    &OutboxRepository{db: tx},
		})
	})
}
