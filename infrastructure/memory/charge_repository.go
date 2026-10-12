package memory

import (
	"context"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type ChargeRepository struct {
	store *Store
	transaction    *transaction
}

func NewChargeRepository(store *Store) *ChargeRepository {
	return &ChargeRepository{store: store}
}

func (r *ChargeRepository) Save(_ context.Context, charge payment.Charge, key transfer.IdempotencyKey, fingerprint transfer.Fingerprint) error {
	return r.store.write(r.transaction, func(transaction *transaction) error {
		return r.store.payments.charges.save(transaction, charge, idempotency{key: key, fingerprint: fingerprint})
	})
}

func (r *ChargeRepository) Update(_ context.Context, charge payment.Charge) error {
	return r.store.write(r.transaction, func(transaction *transaction) error {
		return r.store.payments.charges.update(transaction, charge)
	})
}

func (r *ChargeRepository) FindByID(_ context.Context, id payment.ChargeID) (payment.Charge, error) {
	return r.store.payments.charges.findByID(id)
}

func (r *ChargeRepository) FindByProviderID(_ context.Context, providerID string) (payment.Charge, error) {
	return r.store.payments.charges.lockByProviderID(r.transaction, providerID)
}

func (r *ChargeRepository) FindByIdempotencyKey(_ context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (payment.Charge, transfer.Fingerprint, error) {
	return r.store.payments.charges.findByKey(owner, key)
}

func (r *ChargeRepository) ListPending(_ context.Context, olderThan time.Time, limit int) ([]payment.Charge, error) {
	return r.store.payments.charges.listPending(olderThan, limit), nil
}
