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
	tx := u.store.begin()
	defer tx.release()
	err := work(ctx, transferservice.Store{
		Accounts:  &AccountRepository{store: u.store, tx: tx},
		Ledger:    &LedgerRepository{store: u.store, tx: tx},
		Transfers: &TransferRepository{store: u.store, tx: tx},
		Outbox:    &OutboxRepository{store: u.store, tx: tx},
	})
	if err != nil {
		return err
	}
	return tx.commit()
}
