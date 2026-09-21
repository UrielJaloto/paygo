package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	transferservice "github.com/UrielJaloto/paygo/services/transfer"
)

type TransferUnitOfWork struct {
	pool *pgxpool.Pool
}

func NewTransferUnitOfWork(pool *pgxpool.Pool) *TransferUnitOfWork {
	return &TransferUnitOfWork{pool: pool}
}

func (u *TransferUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store transferservice.Store) error) error {
	return runInTransaction(ctx, u.pool, func(ctx context.Context, transaction pgx.Tx) error {
		return work(ctx, transferservice.Store{
			Accounts:  &AccountRepository{database: transaction},
			Ledger:    &LedgerRepository{database: transaction},
			Transfers: &TransferRepository{database: transaction},
			Outbox:    &OutboxRepository{database: transaction},
		})
	})
}
