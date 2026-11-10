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
	database querier
}

func NewTransferRepository(pool *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{database: pool}
}

func (r *TransferRepository) FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (transfer.Transfer, string, error) {
	var fingerprint string
	found, err := scanTransfer(r.database.QueryRow(ctx, selectTransferByKey, owner, key), &fingerprint)
	return found, fingerprint, mapNotFound(err, transfer.ErrTransferNotFound)
}

func (r *TransferRepository) FindByIDForUpdate(ctx context.Context, id string) (transfer.Transfer, error) {
	found, err := scanTransfer(r.database.QueryRow(ctx, lockTransfer, id))
	return found, mapNotFound(err, transfer.ErrTransferNotFound)
}

func (r *TransferRepository) FindByID(ctx context.Context, id string) (transfer.Transfer, error) {
	found, err := scanTransfer(r.database.QueryRow(ctx, selectTransfer, id))
	return found, mapNotFound(err, transfer.ErrTransferNotFound)
}

func (r *TransferRepository) ListByAccount(ctx context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]transfer.Transfer, error) {
	rows, _ := r.database.Query(ctx, listTransfers, id, cursor.AfterID(), int(limit))
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (transfer.Transfer, error) {
		return scanTransfer(row)
	})
}

func (r *TransferRepository) Save(ctx context.Context, created transfer.Transfer, fingerprint string) error {
	_, err := r.database.Exec(ctx, insertTransfer, created.ID(), created.OwnerID(), created.IdempotencyKey(), fingerprint, created.Kind(), created.From(), created.To(),
		created.Amount().Minor(), created.Amount().Currency(), created.Status(), created.ReversalOf(), created.CreatedAt())
	if isUniqueViolation(err, idempotencyConstraint) {
		return fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, created.IdempotencyKey())
	}
	return err
}

func (r *TransferRepository) MarkReversed(ctx context.Context, reversed transfer.Transfer) error {
	_, err := r.database.Exec(ctx, updateTransfer, reversed.ID(), transfer.StatusReversed)
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
	request.Amount = money.NewMoney(minor, currency)
	return transfer.Restore(snapshot), err
}
