package memory

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type outboxRecord struct {
	lock      sync.Mutex
	event     event.Event
	published bool
}

type OutboxRepository struct {
	store *Store
	tx    *transaction
}

func NewOutboxRepository(store *Store) *OutboxRepository {
	return &OutboxRepository{store: store}
}

func (r *OutboxRepository) Add(_ context.Context, e event.Event) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.writes = append(tx.writes, func() {
			tx.store.events = append(tx.store.events, &outboxRecord{event: e})
		})
		return nil
	})
}

func (r *OutboxRepository) ClaimUnpublished(_ context.Context, limit int) ([]event.Event, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	claimed := make([]event.Event, 0, limit)
	for _, record := range r.store.unpublished() {
		if len(claimed) < limit && r.tx.tryAcquire(&record.lock) {
			claimed = append(claimed, r.store.currentEvent(record.event))
		}
	}
	return claimed, nil
}

func (r *OutboxRepository) MarkPublished(_ context.Context, ids []event.ID) error {
	return r.store.write(r.tx, func(tx *transaction) error {
		tx.writes = append(tx.writes, func() { tx.store.markPublished(ids) })
		return nil
	})
}

func (s *Store) unpublished() []*outboxRecord {
	pending := slices.DeleteFunc(slices.Clone(s.events), func(record *outboxRecord) bool {
		return record.published
	})
	slices.SortFunc(pending, func(a, b *outboxRecord) int {
		return strings.Compare(string(a.event.ID()), string(b.event.ID()))
	})
	return pending
}

func (s *Store) markPublished(ids []event.ID) {
	for _, record := range s.events {
		if slices.Contains(ids, record.event.ID()) {
			record.published = true
		}
	}
}

func (s *Store) currentEvent(e event.Event) event.Event {
	snapshot := event.Snapshot{
		Meta:       event.Meta{ID: e.ID(), At: e.OccurredAt()},
		Type:       e.Type(),
		TransferID: e.TransferID(),
		OwnerIDs:   e.OwnerIDs(),
	}
	if record, found := s.transfers[transfer.ID(e.TransferID())]; found {
		snapshot.Transfer = record.transfer
	}
	return event.Restore(snapshot)
}
