package memory

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type paymentTables struct {
	charges *paymentBook[payment.ChargeID, payment.Charge]
	payouts *paymentBook[payment.PayoutID, payment.Payout]
	events  map[providerEventScope]*providerEventRecord
}

func newPaymentTables(data *sync.RWMutex) paymentTables {
	return paymentTables{
		charges: newPaymentBook(data, payment.ErrChargeNotFound, paymentView[payment.ChargeID, payment.Charge]{
			id:         payment.Charge.ID,
			owner:      payment.Charge.OwnerID,
			providerID: func(c payment.Charge) string { return c.Reference().ProviderID },
			pending:    func(c payment.Charge) bool { return c.Status() == payment.ChargePending },
			createdAt:  func(c payment.Charge) time.Time { return c.Timeline().CreatedAt },
		}),
		payouts: newPaymentBook(data, payment.ErrPayoutNotFound, paymentView[payment.PayoutID, payment.Payout]{
			id:         payment.Payout.ID,
			owner:      payment.Payout.OwnerID,
			providerID: func(p payment.Payout) string { return p.Reference().ProviderID },
			pending:    func(p payment.Payout) bool { return p.Status() == payment.PayoutPending },
			createdAt:  func(p payment.Payout) time.Time { return p.Timeline().CreatedAt },
		}),
		events: map[providerEventScope]*providerEventRecord{},
	}
}

type paymentView[ID ~string, T any] struct {
	id         func(T) ID
	owner      func(T) apikey.OwnerID
	providerID func(T) string
	pending    func(T) bool
	createdAt  func(T) time.Time
}

type paymentRecord[T any] struct {
	lock        sync.Mutex
	item        T
	fingerprint transfer.Fingerprint
}

type idempotency struct {
	key         transfer.IdempotencyKey
	fingerprint transfer.Fingerprint
}

type paymentBook[ID ~string, T any] struct {
	data     *sync.RWMutex
	notFound error
	view     paymentView[ID, T]
	records  map[ID]*paymentRecord[T]
	keys     map[idempotencyScope]ID
}

func newPaymentBook[ID ~string, T any](data *sync.RWMutex, notFound error, view paymentView[ID, T]) *paymentBook[ID, T] {
	return &paymentBook[ID, T]{
		data:     data,
		notFound: notFound,
		view:     view,
		records:  map[ID]*paymentRecord[T]{},
		keys:     map[idempotencyScope]ID{},
	}
}

func (b *paymentBook[ID, T]) save(tx *transaction, item T, claim idempotency) error {
	check := func() error { return b.checkNew(item, claim.key) }
	b.data.RLock()
	err := check()
	b.data.RUnlock()
	if err != nil {
		return err
	}
	tx.checks = append(tx.checks, check)
	tx.writes = append(tx.writes, func() {
		id := b.view.id(item)
		b.records[id] = &paymentRecord[T]{item: item, fingerprint: claim.fingerprint}
		b.keys[idempotencyScope{owner: b.view.owner(item), key: claim.key}] = id
	})
	return nil
}

func (b *paymentBook[ID, T]) checkNew(item T, key transfer.IdempotencyKey) error {
	if _, taken := b.keys[idempotencyScope{owner: b.view.owner(item), key: key}]; taken {
		return fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, key)
	}
	if _, taken := b.records[b.view.id(item)]; taken {
		return ErrDuplicateID
	}
	return nil
}

func (b *paymentBook[ID, T]) update(tx *transaction, item T) error {
	tx.writes = append(tx.writes, func() {
		if record, found := b.records[b.view.id(item)]; found {
			record.item = item
		}
	})
	return nil
}

func (b *paymentBook[ID, T]) findByID(id ID) (T, error) {
	b.data.RLock()
	defer b.data.RUnlock()
	record, found := b.records[id]
	if !found {
		var zero T
		return zero, b.notFound
	}
	return record.item, nil
}

func (b *paymentBook[ID, T]) lockByProviderID(tx *transaction, providerID string) (T, error) {
	match := b.recordByProviderID(providerID)
	if match == nil {
		var zero T
		return zero, b.notFound
	}
	tx.acquire(&match.lock)
	b.data.RLock()
	defer b.data.RUnlock()
	return match.item, nil
}

func (b *paymentBook[ID, T]) recordByProviderID(providerID string) *paymentRecord[T] {
	b.data.RLock()
	defer b.data.RUnlock()
	for _, record := range b.records {
		if b.view.providerID(record.item) == providerID {
			return record
		}
	}
	return nil
}

func (b *paymentBook[ID, T]) findByKey(owner apikey.OwnerID, key transfer.IdempotencyKey) (T, transfer.Fingerprint, error) {
	b.data.RLock()
	defer b.data.RUnlock()
	id, found := b.keys[idempotencyScope{owner: owner, key: key}]
	if !found {
		var zero T
		return zero, "", b.notFound
	}
	record := b.records[id]
	return record.item, record.fingerprint, nil
}

func (b *paymentBook[ID, T]) listPending(olderThan time.Time, limit int) []T {
	b.data.RLock()
	defer b.data.RUnlock()
	pending := make([]T, 0, len(b.records))
	for _, record := range b.records {
		if b.view.pending(record.item) && b.view.createdAt(record.item).Before(olderThan) {
			pending = append(pending, record.item)
		}
	}
	slices.SortFunc(pending, func(x, y T) int {
		return cmp.Or(b.view.createdAt(x).Compare(b.view.createdAt(y)), cmp.Compare(b.view.id(x), b.view.id(y)))
	})
	return pending[:min(len(pending), limit)]
}
