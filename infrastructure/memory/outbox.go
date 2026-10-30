package memory

import (
	"context"

	"github.com/UrielJaloto/paygo/domain/event"
)

type OutboxRepository struct {
	store *Store
	tx    *transaction
}

func NewOutboxRepository(store *Store) *OutboxRepository {
	return &OutboxRepository{store: store}
}

func (r *OutboxRepository) Add(_ context.Context, e event.Event) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.writes = append(tx.writes, func() { tx.store.events = append(tx.store.events, e) })
		return nil
	})
}
