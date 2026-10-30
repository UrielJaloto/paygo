package postgres

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
	"github.com/UrielJaloto/paygo/internal/services/dispatch"
	webhookservice "github.com/UrielJaloto/paygo/internal/services/webhook"
)

const (
	deliveryColumns = "id, event_id, endpoint_id, owner_id, status, attempts, next_attempt_at, last_error, updated_at"
	insertDelivery  = `INSERT INTO webhook_deliveries (` + deliveryColumns + `) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	selectDelivery  = `SELECT ` + deliveryColumns + ` FROM webhook_deliveries WHERE id = $1`
	listDeliveries  = `SELECT ` + deliveryColumns + ` FROM webhook_deliveries
		WHERE owner_id = $1 AND ($2::text = '' OR status = $2::text)
			AND id < COALESCE(NULLIF($3::text, '')::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff')
		ORDER BY id DESC LIMIT $4`
	listPendingDeliveries = `SELECT ` + deliveryColumns + ` FROM webhook_deliveries
		WHERE endpoint_id = $1 AND status = 'pending' ORDER BY id`
	updateDelivery = `UPDATE webhook_deliveries
		SET status = $2, attempts = $3, next_attempt_at = $4, last_error = $5, updated_at = $6 WHERE id = $1`
	claimDeliveries = `WITH claimed AS (
			UPDATE webhook_deliveries SET next_attempt_at = $2
			WHERE id IN (
				SELECT id FROM webhook_deliveries
				WHERE status = 'pending' AND next_attempt_at <= $1
				ORDER BY next_attempt_at LIMIT $3
				FOR UPDATE SKIP LOCKED
			)
			RETURNING ` + deliveryColumns + `
		)
		SELECT c.id, c.event_id, c.endpoint_id, c.owner_id, c.status, c.attempts, c.next_attempt_at, c.last_error, c.updated_at,
			e.id, e.owner_id, e.url, e.secret, e.is_active, e.created_at,
			o.id, o.type, COALESCE(o.transfer_id::text, ''), o.owner_ids::text[], o.occurred_at
		FROM claimed c
		JOIN webhook_endpoints e ON e.id = c.endpoint_id
		JOIN outbox_events o ON o.id = c.event_id
		ORDER BY c.next_attempt_at, c.id`
)

type DeliveryRepository struct {
	database querier
}

func NewDeliveryRepository(pool *pgxpool.Pool) *DeliveryRepository {
	return &DeliveryRepository{database: pool}
}

func (r *DeliveryRepository) FindByID(ctx context.Context, id webhook.DeliveryID) (webhook.Delivery, error) {
	var snapshot webhook.DeliverySnapshot
	err := r.database.QueryRow(ctx, selectDelivery, id).Scan(deliveryDestinations(&snapshot)...)
	return webhook.RestoreDelivery(snapshot), notFound(err, webhook.ErrDeliveryNotFound)
}

func (r *DeliveryRepository) ListByOwner(ctx context.Context, filter webhookservice.DeliveryFilter, cursor page.Cursor, limit page.Limit) ([]webhook.Delivery, error) {
	rows, _ := r.database.Query(ctx, listDeliveries, filter.Owner, filter.Status, cursor.AfterID(), int(limit))
	return pgx.CollectRows(rows, scanDeliveryRow)
}

func (r *DeliveryRepository) ListPendingByEndpoint(ctx context.Context, id webhook.EndpointID) ([]webhook.Delivery, error) {
	rows, _ := r.database.Query(ctx, listPendingDeliveries, id)
	return pgx.CollectRows(rows, scanDeliveryRow)
}

func (r *DeliveryRepository) Update(ctx context.Context, delivery webhook.Delivery) error {
	_, err := r.database.Exec(ctx, updateDelivery, delivery.ID(), delivery.Status(), delivery.Attempts(), delivery.NextAttemptAt(), delivery.LastError(), delivery.UpdatedAt())
	return err
}

func (r *DeliveryRepository) SaveAll(ctx context.Context, deliveries []webhook.Delivery) error {
	batch := &pgx.Batch{}
	for _, d := range deliveries {
		batch.Queue(insertDelivery, d.ID(), d.EventID(), d.EndpointID(), d.OwnerID(), d.Status(), d.Attempts(), d.NextAttemptAt(), d.LastError(), d.UpdatedAt())
	}
	return r.database.SendBatch(ctx, batch).Close()
}

func (r *DeliveryRepository) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]dispatch.Claim, error) {
	rows, _ := r.database.Query(ctx, claimDeliveries, now, now.Add(lease), limit)
	claimed, err := pgx.CollectRows(rows, scanClaimRow)
	if err != nil {
		return nil, err
	}
	stored := make([]storedEvent, 0, len(claimed))
	for _, row := range claimed {
		stored = append(stored, row.event)
	}
	events, err := restoreEvents(ctx, r.database, stored)
	if err != nil {
		return nil, err
	}
	claims := make([]dispatch.Claim, 0, len(claimed))
	for position, row := range claimed {
		claims = append(claims, dispatch.Claim{
			Delivery: webhook.RestoreDelivery(row.delivery),
			Endpoint: webhook.RestoreEndpoint(row.endpoint),
			Event:    events[position],
		})
	}
	return claims, nil
}

type claimRow struct {
	delivery webhook.DeliverySnapshot
	endpoint webhook.EndpointSnapshot
	event    storedEvent
}

func scanClaimRow(row pgx.CollectableRow) (claimRow, error) {
	var claimed claimRow
	destinations := slices.Concat(deliveryDestinations(&claimed.delivery), endpointDestinations(&claimed.endpoint), claimed.event.scanEventTargets())
	err := row.Scan(destinations...)
	return claimed, err
}

func scanDeliveryRow(row pgx.CollectableRow) (webhook.Delivery, error) {
	var snapshot webhook.DeliverySnapshot
	err := row.Scan(deliveryDestinations(&snapshot)...)
	return webhook.RestoreDelivery(snapshot), err
}

func deliveryDestinations(snapshot *webhook.DeliverySnapshot) []any {
	return []any{&snapshot.ID, &snapshot.EventID, &snapshot.EndpointID, &snapshot.OwnerID, &snapshot.Status,
		&snapshot.Attempts, &snapshot.NextAttemptAt, &snapshot.LastError, &snapshot.UpdatedAt}
}
