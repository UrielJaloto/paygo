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
	payoutColumns = `id, owner_id, account_id, amount_minor, currency, pix_key, pix_key_type, provider,
		COALESCE(provider_id, ''), hold_transfer_id, COALESCE(final_transfer_id::text, ''), status, created_at, updated_at`
	insertPayout = `INSERT INTO payouts (id, owner_id, idempotency_key, fingerprint, account_id, amount_minor, currency,
			pix_key, pix_key_type, provider, provider_id, hold_transfer_id, final_transfer_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11::text, ''), $12, NULLIF($13::text, '')::uuid, $14, $15, $16)`
	updatePayout = `UPDATE payouts SET provider_id = NULLIF($2::text, ''), final_transfer_id = NULLIF($3::text, '')::uuid, status = $4, updated_at = $5
		WHERE id = $1`
	selectPayout           = `SELECT ` + payoutColumns + ` FROM payouts WHERE id = $1`
	lockPayoutByProviderID = `SELECT ` + payoutColumns + ` FROM payouts WHERE provider_id = $1 FOR UPDATE`
	selectPayoutByKey      = `SELECT ` + payoutColumns + `, fingerprint FROM payouts WHERE owner_id = $1 AND idempotency_key = $2`
	listPendingPayouts     = `SELECT ` + payoutColumns + ` FROM payouts
		WHERE status = 'pending' AND created_at < $1 ORDER BY created_at LIMIT $2`
	payoutIdempotencyConstraint = "payouts_owner_id_idempotency_key_key"
)

type PayoutRepository struct {
	database querier
}

func NewPayoutRepository(pool *pgxpool.Pool) *PayoutRepository {
	return &PayoutRepository{database: pool}
}

func (r *PayoutRepository) Save(ctx context.Context, payout payment.Payout, key transfer.IdempotencyKey, fingerprint transfer.Fingerprint) error {
	_, err := r.database.Exec(ctx, insertPayout, payout.ID(), payout.OwnerID(), key, fingerprint, payout.AccountID(),
		payout.Amount().Minor(), payout.Amount().Currency(), payout.PixKey().Value(), payout.PixKey().Kind(),
		payout.Reference().Provider, payout.Reference().ProviderID, payout.Transfers().Hold, payout.Transfers().Final,
		payout.Status(), payout.Timeline().CreatedAt, payout.Timeline().UpdatedAt)
	if isUniqueViolation(err, payoutIdempotencyConstraint) {
		return fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, key)
	}
	return err
}

func (r *PayoutRepository) Update(ctx context.Context, payout payment.Payout) error {
	_, err := r.database.Exec(ctx, updatePayout, payout.ID(), payout.Reference().ProviderID, payout.Transfers().Final, payout.Status(), payout.Timeline().UpdatedAt)
	return err
}

func (r *PayoutRepository) FindByID(ctx context.Context, id payment.PayoutID) (payment.Payout, error) {
	found, err := scanPayout(r.database.QueryRow(ctx, selectPayout, id))
	return found, mapNotFound(err, payment.ErrPayoutNotFound)
}

func (r *PayoutRepository) FindByProviderID(ctx context.Context, providerID string) (payment.Payout, error) {
	found, err := scanPayout(r.database.QueryRow(ctx, lockPayoutByProviderID, providerID))
	return found, mapNotFound(err, payment.ErrPayoutNotFound)
}

func (r *PayoutRepository) FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (payment.Payout, transfer.Fingerprint, error) {
	var fingerprint transfer.Fingerprint
	found, err := scanPayout(r.database.QueryRow(ctx, selectPayoutByKey, owner, key), &fingerprint)
	return found, fingerprint, mapNotFound(err, payment.ErrPayoutNotFound)
}

func (r *PayoutRepository) ListPending(ctx context.Context, olderThan time.Time, limit int) ([]payment.Payout, error) {
	rows, _ := r.database.Query(ctx, listPendingPayouts, olderThan, limit)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (payment.Payout, error) {
		return scanPayout(row)
	})
}

func scanPayout(row pgx.Row, extra ...any) (payment.Payout, error) {
	var snapshot payment.PayoutSnapshot
	var minor int64
	var currency money.Currency
	var keyValue, keyKind string
	destinations := []any{&snapshot.ID, &snapshot.OwnerID, &snapshot.AccountID, &minor, &currency, &keyValue, &keyKind,
		&snapshot.Reference.Provider, &snapshot.Reference.ProviderID, &snapshot.Transfers.Hold, &snapshot.Transfers.Final,
		&snapshot.Status, &snapshot.Timeline.CreatedAt, &snapshot.Timeline.UpdatedAt}
	err := row.Scan(append(destinations, extra...)...)
	snapshot.Amount = money.NewMoney(minor, currency)
	snapshot.PixKey, _ = payment.ParsePixKey(keyKind, keyValue)
	return payment.RestorePayout(snapshot), err
}
