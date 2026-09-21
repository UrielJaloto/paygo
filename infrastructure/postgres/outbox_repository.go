package postgres

import (
	"context"

	"github.com/UrielJaloto/paygo/domain/event"
)

const insertOutboxEvent = `INSERT INTO outbox_events (id, type, transfer_id, owner_ids, occurred_at)
	VALUES ($1, $2, NULLIF($3::text, '')::uuid, $4::uuid[], $5)`

type OutboxRepository struct {
	database querier
}

func (r *OutboxRepository) Add(ctx context.Context, e event.Event) error {
	_, err := r.database.Exec(ctx, insertOutboxEvent, e.ID(), e.Type(), e.TransferID(), e.OwnerIDs(), e.OccurredAt())
	return err
}
