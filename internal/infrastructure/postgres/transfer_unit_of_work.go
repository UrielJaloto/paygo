package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
)

type TransferUnitOfWork struct {
	pool *pgxpool.Pool
}

func NewTransferUnitOfWork(pool *pgxpool.Pool) *TransferUnitOfWork {
	return &TransferUnitOfWork{pool: pool}
}

func (u *TransferUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store transferservice.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, tx pgx.Tx) error {
		return work(ctx, transferservice.Store{
			Accounts:  &AccountRepository{db: tx},
			Ledger:    &LedgerRepository{db: tx},
			Transfers: &TransferRepository{db: tx},
			Outbox:    &OutboxRepository{db: tx},
		})
	})
}
