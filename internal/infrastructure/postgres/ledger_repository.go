package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/services/ledgercheck"
)

const (
	signedAmount  = `CASE direction WHEN 'credit' THEN amount_minor ELSE -amount_minor END`
	selectBalance = `SELECT balance_minor, currency FROM accounts WHERE id = $1`
	selectEntries = `SELECT id, transfer_id, account_id, direction, amount_minor, currency, created_at FROM ledger_entries
		WHERE account_id = $1 AND id < COALESCE(NULLIF($2::text, '')::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff')
		ORDER BY id DESC LIMIT $3`
	appendJournal = `
		WITH new_entries AS (
			SELECT * FROM unnest($2::uuid[], $3::uuid[], $4::text[], $5::bigint[], $6::text[], $7::timestamptz[])
				AS entry (id, account_id, direction, amount_minor, currency, created_at)
		), inserted AS (
			INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount_minor, currency, created_at)
			SELECT id, $1, account_id, direction, amount_minor, currency, created_at FROM new_entries
		)
		UPDATE accounts SET balance_minor = balance_minor + delta.minor
		FROM (
			SELECT account_id, sum(` + signedAmount + `) AS minor FROM new_entries GROUP BY account_id
		) delta
		WHERE accounts.id = delta.account_id`
	selectMismatches = `
		SELECT a.id, a.balance_minor, COALESCE(sum(` + signedAmount + `), 0)::bigint
		FROM accounts a LEFT JOIN ledger_entries e ON e.account_id = a.id
		GROUP BY a.id
		HAVING a.balance_minor <> COALESCE(sum(` + signedAmount + `), 0)
		ORDER BY a.id`
	countAccounts          = `SELECT count(*) FROM accounts`
	totalEntriesByCurrency = `SELECT currency, sum(` + signedAmount + `)::bigint FROM ledger_entries GROUP BY currency`
)

type LedgerRepository struct {
	database querier
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{database: pool}
}

func (r *LedgerRepository) FindBalance(ctx context.Context, id account.ID) (money.Money, error) {
	var minor int64
	var currency money.Currency
	err := r.database.QueryRow(ctx, selectBalance, id).Scan(&minor, &currency)
	return money.NewMoney(minor, currency), mapNotFound(err, account.ErrAccountNotFound)
}

func (r *LedgerRepository) ListStatement(ctx context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]ledger.Entry, error) {
	rows, _ := r.database.Query(ctx, selectEntries, id, cursor.AfterID(), int(limit))
	return pgx.CollectRows(rows, scanEntryRow)
}

func (r *LedgerRepository) Append(ctx context.Context, journal ledger.JournalTransaction) error {
	columns := newEntryColumns(journal.Entries())
	_, err := r.database.Exec(ctx, appendJournal, journal.ID(), columns.ids, columns.accounts, columns.directions, columns.amounts, columns.currencies, columns.createdAt)
	return err
}

func (r *LedgerRepository) ListBalanceMismatches(ctx context.Context) ([]ledgercheck.Mismatch, int, error) {
	rows, _ := r.database.Query(ctx, selectMismatches)
	mismatches, mismatchErr := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledgercheck.Mismatch, error) {
		var mismatch ledgercheck.Mismatch
		err := row.Scan(&mismatch.AccountID, &mismatch.StoredMinor, &mismatch.CalculatedMinor)
		return mismatch, err
	})
	var accounts int
	countErr := r.database.QueryRow(ctx, countAccounts).Scan(&accounts)
	return mismatches, accounts, errors.Join(mismatchErr, countErr)
}

func (r *LedgerRepository) CalculateTotalsByCurrency(ctx context.Context) (map[string]int64, error) {
	rows, _ := r.database.Query(ctx, totalEntriesByCurrency)
	totals := map[string]int64{}
	var currency string
	var sum int64
	_, err := pgx.ForEachRow(rows, []any{&currency, &sum}, func() error {
		totals[currency] = sum
		return nil
	})
	return totals, err
}

type entryColumns struct {
	ids        []string
	accounts   []string
	directions []string
	amounts    []int64
	currencies []string
	createdAt  []time.Time
}

func newEntryColumns(entries []ledger.Entry) entryColumns {
	var columns entryColumns
	for _, entry := range entries {
		columns.ids = append(columns.ids, entry.ID())
		columns.accounts = append(columns.accounts, string(entry.AccountID()))
		columns.directions = append(columns.directions, string(entry.Direction()))
		columns.amounts = append(columns.amounts, entry.Amount().Minor())
		columns.currencies = append(columns.currencies, string(entry.Amount().Currency()))
		columns.createdAt = append(columns.createdAt, entry.CreatedAt())
	}
	return columns
}

func scanEntryRow(row pgx.CollectableRow) (ledger.Entry, error) {
	var id, journalID string
	var accountID account.ID
	var direction string
	var minor int64
	var currency money.Currency
	var createdAt time.Time
	err := row.Scan(&id, &journalID, &accountID, &direction, &minor, &currency, &createdAt)
	entry, entryErr := ledger.NewEntry(id, accountID, direction, money.NewMoney(minor, currency), createdAt)
	return entry.AssignJournal(journalID), errors.Join(err, entryErr)
}
