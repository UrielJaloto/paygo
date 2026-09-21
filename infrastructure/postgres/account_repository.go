package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
)

const (
	accountColumns = "id, owner_id, currency, kind, status, version, created_at"
	insertAccount  = `INSERT INTO accounts (` + accountColumns + `) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	selectAccount  = `SELECT ` + accountColumns + ` FROM accounts WHERE id = $1`
	listAccounts   = `SELECT ` + accountColumns + ` FROM accounts
		WHERE owner_id = $1 AND kind = 'customer' AND id > COALESCE(NULLIF($2::text, '')::uuid, '00000000-0000-0000-0000-000000000000')
		ORDER BY id LIMIT $3`
	updateAccount = `UPDATE accounts SET status = $3, version = $4 WHERE id = $1 AND version = $2`
	lockAccounts  = `SELECT ` + accountColumns + ` FROM accounts WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`
)

type AccountRepository struct {
	database querier
}

func NewAccountRepository(pool *pgxpool.Pool) *AccountRepository {
	return &AccountRepository{database: pool}
}

func (r *AccountRepository) Save(ctx context.Context, acc account.Account) error {
	_, err := r.database.Exec(ctx, insertAccount, acc.ID(), acc.OwnerID(), acc.Currency(), acc.Kind(), acc.Status(), acc.Version(), acc.CreatedAt())
	return err
}

func (r *AccountRepository) FindByID(ctx context.Context, id account.ID) (account.Account, error) {
	found, err := scanAccount(r.database.QueryRow(ctx, selectAccount, id))
	return found, notFound(err, account.ErrAccountNotFound)
}

func (r *AccountRepository) ListByOwner(ctx context.Context, owner apikey.OwnerID, cursor page.Cursor, limit page.Limit) ([]account.Account, error) {
	rows, _ := r.database.Query(ctx, listAccounts, owner, cursor.AfterID(), int(limit))
	return pgx.CollectRows(rows, scanAccountRow)
}

func (r *AccountRepository) Update(ctx context.Context, acc account.Account, expectedVersion int64) error {
	updated, err := r.database.Exec(ctx, updateAccount, acc.ID(), expectedVersion, acc.Status(), acc.Version())
	if err != nil {
		return err
	}
	if updated.RowsAffected() == 0 {
		return account.ErrConcurrentModification
	}
	return nil
}

func (r *AccountRepository) LockByIDs(ctx context.Context, ids []account.ID) ([]account.Account, error) {
	rows, _ := r.database.Query(ctx, lockAccounts, accountIDStrings(ids))
	locked, err := pgx.CollectRows(rows, scanAccountRow)
	if err != nil {
		return nil, err
	}
	if len(locked) < len(ids) {
		return nil, account.ErrAccountNotFound
	}
	return locked, nil
}

func accountIDStrings(ids []account.ID) []string {
	texts := make([]string, 0, len(ids))
	for _, id := range ids {
		texts = append(texts, string(id))
	}
	return texts
}

func scanAccountRow(row pgx.CollectableRow) (account.Account, error) {
	return scanAccount(row)
}

func scanAccount(row pgx.Row) (account.Account, error) {
	var snapshot account.Snapshot
	err := row.Scan(&snapshot.ID, &snapshot.OwnerID, &snapshot.Currency, &snapshot.Kind, &snapshot.Status, &snapshot.Version, &snapshot.CreatedAt)
	return account.Restore(snapshot), err
}
