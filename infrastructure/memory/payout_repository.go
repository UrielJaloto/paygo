package memory

import (
	"context"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type PayoutRepository struct {
	store *Store
	tx    *transaction
}

func NewPayoutRepository(store *Store) *PayoutRepository {
	return &PayoutRepository{store: store}
}

func (r *PayoutRepository) Save(_ context.Context, payout payment.Payout, key transfer.IdempotencyKey, fingerprint transfer.Fingerprint) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		return r.store.payments.payouts.save(tx, payout, idempotency{key: key, fingerprint: fingerprint})
	})
}

func (r *PayoutRepository) Update(_ context.Context, payout payment.Payout) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		return r.store.payments.payouts.update(tx, payout)
	})
}

func (r *PayoutRepository) FindByID(_ context.Context, id payment.PayoutID) (payment.Payout, error) {
	return r.store.payments.payouts.findByID(id)
}

func (r *PayoutRepository) FindByProviderID(_ context.Context, providerID string) (payment.Payout, error) {
	return r.store.payments.payouts.lockByProviderID(r.tx, providerID)
}

func (r *PayoutRepository) FindByIdempotencyKey(_ context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (payment.Payout, transfer.Fingerprint, error) {
	return r.store.payments.payouts.findByKey(owner, key)
}

func (r *PayoutRepository) ListPending(_ context.Context, olderThan time.Time, limit int) ([]payment.Payout, error) {
	return r.store.payments.payouts.listPending(olderThan, limit), nil
}
