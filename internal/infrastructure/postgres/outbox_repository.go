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
	database querier
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{database: pool}
}

func (r *OutboxRepository) Add(ctx context.Context, event event.Event) error {
	_, err := r.database.Exec(ctx, insertOutboxEvent, event.ID(), event.Type(), event.TransferID(), event.OwnerIDs(), event.OccurredAt())
	return err
}

func (r *OutboxRepository) ClaimUnpublished(ctx context.Context, limit int) ([]event.Event, error) {
	rows, _ := r.database.Query(ctx, claimOutboxEvents, limit)
	stored, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedEvent, error) {
		var scanned storedEvent
		err := row.Scan(scanned.scanEventTargets()...)
		return scanned, err
	})
	if err != nil {
		return nil, err
	}
	return restoreEvents(ctx, r.database, stored)
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, ids []string) error {
	_, err := r.database.Exec(ctx, markOutboxPublished, ids)
	return err
}

type storedEvent struct {
	metadata   event.Metadata
	eventType  string
	transferID string
	owners     []apikey.OwnerID
}

func (s *storedEvent) scanEventTargets() []any {
	return []any{&s.metadata.ID, &s.eventType, &s.transferID, &s.owners, &s.metadata.At}
}

func restoreEvents(ctx context.Context, database querier, stored []storedEvent) ([]event.Event, error) {
	ids := make([]string, 0, len(stored))
	for _, stored := range stored {
		ids = append(ids, stored.transferID)
	}
	transfers, err := findTransfersByID(ctx, database, ids)
	if err != nil {
		return nil, err
	}
	events := make([]event.Event, 0, len(stored))
	for _, stored := range stored {
		events = append(events, event.Restore(event.Snapshot{
			Metadata:   stored.metadata,
			Type:       stored.eventType,
			Transfer:   transfers[stored.transferID],
			TransferID: string(stored.transferID),
			OwnerIDs:   stored.owners,
		}))
	}
	return events, nil
}

func findTransfersByID(ctx context.Context, database querier, ids []string) (map[string]transfer.Transfer, error) {
	present := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			present = append(present, id)
		}
	}
	rows, _ := database.Query(ctx, selectTransfers, present)
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (transfer.Transfer, error) {
		return scanTransfer(row)
	})
	byID := make(map[string]transfer.Transfer, len(found))
	for _, transfer := range found {
		byID[transfer.ID()] = transfer
	}
	return byID, err
}
