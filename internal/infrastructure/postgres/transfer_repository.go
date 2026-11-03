package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const (
	transferColumns = `id, owner_id, idempotency_key, kind, from_account_id, to_account_id,
		amount_minor, currency, status, COALESCE(reversal_of::text, ''), created_at`
	insertTransfer = `INSERT INTO transfers (id, owner_id, idempotency_key, fingerprint, kind, from_account_id, to_account_id,
			amount_minor, currency, status, reversal_of, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11::text, '')::uuid, $12)`
	selectTransferByKey = `SELECT ` + transferColumns + `, fingerprint FROM transfers WHERE owner_id = $1 AND idempotency_key = $2`
	selectTransfer      = `SELECT ` + transferColumns + ` FROM transfers WHERE id = $1`
	lockTransfer        = selectTransfer + ` FOR UPDATE`
	updateTransfer      = `UPDATE transfers SET status = $2 WHERE id = $1`
	listTransfers       = `SELECT ` + transferColumns + ` FROM transfers
		WHERE (from_account_id = $1 OR to_account_id = $1)
			AND id < COALESCE(NULLIF($2::text, '')::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff')
		ORDER BY id DESC LIMIT $3`
	idempotencyConstraint = "transfers_owner_id_idempotency_key_key"
)

type TransferRepository struct {
	db querier
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{db: pool}
}

func (r *TransferRepository) FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (transfer.Transfer, transfer.Fingerprint, error) {
	var fingerprint transfer.Fingerprint
	found, err := scanTransfer(r.db.QueryRow(ctx, selectTransferByKey, owner, key), &fingerprint)
	return found, fingerprint, notFound(err, transfer.ErrTransferNotFound)
}

func (r *TransferRepository) FindByIDForUpdate(ctx context.Context, id transfer.ID) (transfer.Transfer, error) {
	found, err := scanTransfer(r.db.QueryRow(ctx, lockTransfer, id))
	return found, notFound(err, transfer.ErrTransferNotFound)
}

func (r *TransferRepository) FindByID(ctx context.Context, id transfer.ID) (transfer.Transfer, error) {
	found, err := scanTransfer(r.db.QueryRow(ctx, selectTransfer, id))
	return found, notFound(err, transfer.ErrTransferNotFound)
}

func (r *TransferRepository) ListByAccount(ctx context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]transfer.Transfer, error) {
	rows, _ := r.db.Query(ctx, listTransfers, id, cursor.AfterID(), int(limit))
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (transfer.Transfer, error) {
		return scanTransfer(row)
	})
}

func (r *TransferRepository) Save(ctx context.Context, t transfer.Transfer, fingerprint transfer.Fingerprint) error {
	_, err := r.db.Exec(ctx, insertTransfer, t.ID(), t.OwnerID(), t.IdempotencyKey(), fingerprint, t.Kind(), t.From(), t.To(),
		t.Amount().Minor(), t.Amount().Currency(), t.Status(), t.ReversalOf(), t.CreatedAt())
	if isUniqueViolation(err, idempotencyConstraint) {
		return fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, t.IdempotencyKey())
	}
	return err
}

func (r *TransferRepository) MarkReversed(ctx context.Context, t transfer.Transfer) error {
	_, err := r.db.Exec(ctx, updateTransfer, t.ID(), transfer.StatusReversed)
	return err
}

func scanTransfer(row pgx.Row, extra ...any) (transfer.Transfer, error) {
	var snapshot transfer.Snapshot
	var minor int64
	var currency money.Currency
	request := &snapshot.Request
	destinations := []any{&snapshot.ID, &request.OwnerID, &request.Key, &request.Kind, &request.From, &request.To,
		&minor, &currency, &snapshot.Status, &request.ReversalOf, &snapshot.CreatedAt}
	err := row.Scan(append(destinations, extra...)...)
	request.Amount = money.New(minor, currency)
	return transfer.Restore(snapshot), err
}
