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
	transaction := u.store.begin()
	defer transaction.release()
	err := work(ctx, paymentservice.Store{
		Accounts:  &AccountRepository{store: u.store, transaction: transaction},
		Ledger:    &LedgerRepository{store: u.store, transaction: transaction},
		Transfers: &TransferRepository{store: u.store, transaction: transaction},
		Charges:   &ChargeRepository{store: u.store, transaction: transaction},
		Payouts:   &PayoutRepository{store: u.store, transaction: transaction},
		Events:    &ProviderEventLog{store: u.store, transaction: transaction},
		Outbox:    &OutboxRepository{store: u.store, transaction: transaction},
	})
	if err != nil {
		return err
	}
	return transaction.commit()
}
