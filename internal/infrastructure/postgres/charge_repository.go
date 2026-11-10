package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const (
	chargeColumns = `id, owner_id, account_id, amount_minor, currency, payer_document, payer_name, provider,
		COALESCE(provider_id, ''), pix_copy_paste, pix_qr_base64, status, COALESCE(transfer_id::text, ''),
		expires_at, created_at, updated_at`
	insertCharge = `INSERT INTO charges (id, owner_id, idempotency_key, fingerprint, account_id, amount_minor, currency,
			payer_document, payer_name, provider, provider_id, pix_copy_paste, pix_qr_base64, status, transfer_id,
			expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11::text, ''), $12, $13, $14,
			NULLIF($15::text, '')::uuid, $16, $17, $18)`
	updateCharge = `UPDATE charges SET provider_id = NULLIF($2::text, ''), pix_copy_paste = $3, pix_qr_base64 = $4, status = $5,
			transfer_id = NULLIF($6::text, '')::uuid, updated_at = $7
		WHERE id = $1`
	selectCharge           = `SELECT ` + chargeColumns + ` FROM charges WHERE id = $1`
	lockChargeByProviderID = `SELECT ` + chargeColumns + ` FROM charges WHERE provider_id = $1 FOR UPDATE`
	selectChargeByKey      = `SELECT ` + chargeColumns + `, fingerprint FROM charges WHERE owner_id = $1 AND idempotency_key = $2`
	listPendingCharges     = `SELECT ` + chargeColumns + ` FROM charges
		WHERE status = 'pending' AND created_at < $1 ORDER BY created_at LIMIT $2`
	chargeIdempotencyConstraint = "charges_owner_id_idempotency_key_key"
)

type ChargeRepository struct {
	database querier
}

func NewChargeRepository(pool *pgxpool.Pool) *ChargeRepository {
	return &ChargeRepository{database: pool}
}

func (r *ChargeRepository) Save(ctx context.Context, charge payment.Charge, key transfer.IdempotencyKey, fingerprint string) error {
	_, err := r.database.Exec(ctx, insertCharge, charge.ID(), charge.OwnerID(), key, fingerprint, charge.AccountID(),
		charge.Amount().Minor(), charge.Amount().Currency(), charge.Payer().Document, charge.Payer().Name,
		charge.Reference().Provider, charge.Reference().ProviderID, charge.Pix().CopyPaste, charge.Pix().QRBase64,
		charge.Status(), charge.TransferID(), charge.Pix().ExpiresAt, charge.Timeline().CreatedAt, charge.Timeline().UpdatedAt)
	if isUniqueViolation(err, chargeIdempotencyConstraint) {
		return fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, key)
	}
	return err
}

func (r *ChargeRepository) Update(ctx context.Context, charge payment.Charge) error {
	_, err := r.database.Exec(ctx, updateCharge, charge.ID(), charge.Reference().ProviderID, charge.Pix().CopyPaste, charge.Pix().QRBase64,
		charge.Status(), charge.TransferID(), charge.Timeline().UpdatedAt)
	return err
}

func (r *ChargeRepository) FindByID(ctx context.Context, id string) (payment.Charge, error) {
	found, err := scanCharge(r.database.QueryRow(ctx, selectCharge, id))
	return found, mapNotFound(err, payment.ErrChargeNotFound)
}

func (r *ChargeRepository) FindByProviderID(ctx context.Context, providerID string) (payment.Charge, error) {
	found, err := scanCharge(r.database.QueryRow(ctx, lockChargeByProviderID, providerID))
	return found, mapNotFound(err, payment.ErrChargeNotFound)
}

func (r *ChargeRepository) FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (payment.Charge, string, error) {
	var fingerprint string
	found, err := scanCharge(r.database.QueryRow(ctx, selectChargeByKey, owner, key), &fingerprint)
	return found, fingerprint, mapNotFound(err, payment.ErrChargeNotFound)
}

func (r *ChargeRepository) ListPending(ctx context.Context, olderThan time.Time, limit int) ([]payment.Charge, error) {
	rows, _ := r.database.Query(ctx, listPendingCharges, olderThan, limit)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (payment.Charge, error) {
		return scanCharge(row)
	})
}

func scanCharge(row pgx.Row, extra ...any) (payment.Charge, error) {
	var snapshot payment.ChargeSnapshot
	var minor int64
	var currency money.Currency
	destinations := []any{&snapshot.ID, &snapshot.OwnerID, &snapshot.AccountID, &minor, &currency,
		&snapshot.Payer.Document, &snapshot.Payer.Name, &snapshot.Reference.Provider, &snapshot.Reference.ProviderID,
		&snapshot.Pix.CopyPaste, &snapshot.Pix.QRBase64, &snapshot.Status, &snapshot.TransferID,
		&snapshot.Pix.ExpiresAt, &snapshot.Timeline.CreatedAt, &snapshot.Timeline.UpdatedAt}
	err := row.Scan(append(destinations, extra...)...)
	snapshot.Amount = money.NewMoney(minor, currency)
	return payment.RestoreCharge(snapshot), err
}
