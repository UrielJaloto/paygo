package memory

import (
	"context"

	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

type PaymentUnitOfWork struct {
	store *Store
}

func NewPaymentUnitOfWork(store *Store) *PaymentUnitOfWork {
	return &PaymentUnitOfWork{store: store}
}

func (u *PaymentUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store paymentservice.Store) error) error {
	tx := u.store.begin()
	defer tx.release()
	err := work(ctx, paymentservice.Store{
		Accounts:  &AccountRepository{store: u.store, tx: tx},
		Ledger:    &LedgerRepository{store: u.store, tx: tx},
		Transfers: &TransferRepository{store: u.store, tx: tx},
		Charges:   &ChargeRepository{store: u.store, tx: tx},
		Payouts:   &PayoutRepository{store: u.store, tx: tx},
		Events:    &ProviderEventLog{store: u.store, tx: tx},
		Outbox:    &OutboxRepository{store: u.store, tx: tx},
	})
	if err != nil {
		return err
	}
	return tx.commit()
}
