package memory

import (
	"context"

	"github.com/UrielJaloto/paygo/domain/event"
)

type OutboxRepository struct {
	store *Store
	transaction    *transaction
}

func NewOutboxRepository(store *Store) *OutboxRepository {
	return &OutboxRepository{store: store}
}

func (r *OutboxRepository) Add(_ context.Context, e event.Event) error {
	return r.store.write(r.transaction, func(transaction *transaction) error {
		transaction.writes = append(transaction.writes, func() { transaction.store.events = append(transaction.store.events, e) })
		return nil
	})
}
