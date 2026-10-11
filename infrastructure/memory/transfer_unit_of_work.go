package memory

import (
	"context"

	transferservice "github.com/UrielJaloto/paygo/services/transfer"
)

type TransferUnitOfWork struct {
	store *Store
}

func NewTransferUnitOfWork(store *Store) *TransferUnitOfWork {
	return &TransferUnitOfWork{store: store}
}

func (u *TransferUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store transferservice.Store) error) error {
	transaction := u.store.begin()
	defer transaction.release()
	err := work(ctx, transferservice.Store{
		Accounts:  &AccountRepository{store: u.store, transaction: transaction},
		Ledger:    &LedgerRepository{store: u.store, transaction: transaction},
		Transfers: &TransferRepository{store: u.store, transaction: transaction},
		Outbox:    &OutboxRepository{store: u.store, transaction: transaction},
	})
	if err != nil {
		return err
	}
	return transaction.commit()
}
