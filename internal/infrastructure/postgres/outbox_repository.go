package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const (
	insertOutboxEvent = `INSERT INTO outbox_events (id, type, transfer_id, owner_ids, occurred_at)
		VALUES ($1, $2, NULLIF($3::text, '')::uuid, $4::text[]::uuid[], $5)`
	claimOutboxEvents = `SELECT id, type, COALESCE(transfer_id::text, ''), owner_ids::text[], occurred_at
		FROM outbox_events WHERE published_at IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`
	markOutboxPublished = `UPDATE outbox_events SET published_at = now() WHERE id = ANY($1::text[]::uuid[])`
	selectTransfers     = `SELECT ` + transferColumns + ` FROM transfers WHERE id = ANY($1::text[]::uuid[])`
)

type OutboxRepository struct {
	db querier
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{db: pool}
}

func (r *OutboxRepository) Add(ctx context.Context, e event.Event) error {
	_, err := r.db.Exec(ctx, insertOutboxEvent, e.ID(), e.Type(), e.TransferID(), e.OwnerIDs(), e.OccurredAt())
	return err
}

func (r *OutboxRepository) ClaimUnpublished(ctx context.Context, limit int) ([]event.Event, error) {
	rows, _ := r.db.Query(ctx, claimOutboxEvents, limit)
	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedEvent, error) {
		var scanned storedEvent
		err := row.Scan(scanned.destinations()...)
		return scanned, err
	})
	if err != nil {
		return nil, err
	}
	return restoreEvents(ctx, r.db, stored)
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, ids []event.ID) error {
	_, err := r.db.Exec(ctx, markOutboxPublished, ids)
	return err
}

type storedEvent struct {
	meta       event.Meta
	eventType  event.Type
	transferID transfer.ID
	owners     []apikey.OwnerID
}

func (s *storedEvent) destinations() []any {
	return []any{&s.meta.ID, &s.eventType, &s.transferID, &s.owners, &s.meta.At}
}

func restoreEvents(ctx context.Context, db querier, stored []storedEvent) ([]event.Event, error) {
	ids := make([]transfer.ID, 0, len(stored))
	for _, s := range stored {
		ids = append(ids, s.transferID)
	}
	transfers, err := transfersByID(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	events := make([]event.Event, 0, len(stored))
	for _, s := range stored {
		events = append(events, event.Restore(event.Snapshot{
			Meta:       s.meta,
			Type:       s.eventType,
			Transfer:   transfers[s.transferID],
			TransferID: string(s.transferID),
			OwnerIDs:   s.owners,
		}))
	}
	return events, nil
}

func transfersByID(ctx context.Context, db querier, ids []transfer.ID) (map[transfer.ID]transfer.Transfer, error) {
	present := make([]transfer.ID, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			present = append(present, id)
		}
	}
	rows, _ := db.Query(ctx, selectTransfers, present)
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (transfer.Transfer, error) {
		return scanTransfer(row)
	})
	byID := make(map[transfer.ID]transfer.Transfer, len(found))
	for _, t := range found {
		byID[t.ID()] = t
	}
	return byID, err
}
