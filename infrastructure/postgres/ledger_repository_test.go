//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

func appendTransfer(t *testing.T, repository *postgres.LedgerRepository, base int, from, to account.ID) {
	t.Helper()
	journal, err := ledger.Posting{
		ID:       testID(base),
		EntryIDs: [2]string{testID(base + 1), testID(base + 2)},
		From:     from,
		To:       to,
		Amount:   money.NewMoney(100, money.BRL),
		At:       testTime,
	}.BuildJournal()
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if err := repository.Append(context.Background(), journal); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
}

func newLedgerFixture(t *testing.T) (*pgxpool.Pool, *postgres.LedgerRepository, account.ID, account.ID) {
	t.Helper()
	pool := newTestPool(t)
	accounts := postgres.NewAccountRepository(pool)
	return pool, postgres.NewLedgerRepository(pool), saveCustomer(t, accounts, 1, testOwner).ID(), saveCustomer(t, accounts, 2, testOwner).ID()
}

func assertBalance(t *testing.T, repository *postgres.LedgerRepository, id account.ID, want int64) {
	t.Helper()
	balance, err := repository.CalculateBalance(context.Background(), id)
	if err != nil {
		t.Fatalf("CalculateBalance() error = %v", err)
	}
	if balance.Minor() != want || balance.Currency() != money.BRL {
		t.Fatalf("balance = %s, want %d BRL", balance, want)
	}
}

func TestLedgerAppend_Transfer_UpdatesBalancesAndStoresEntries(t *testing.T) {
	_, repository, from, to := newLedgerFixture(t)
	appendTransfer(t, repository, 100, from, to)

	assertBalance(t, repository, from, -100)
	assertBalance(t, repository, to, 100)
	entries, err := repository.ListStatement(context.Background(), to, page.Cursor{}, 10)
	if err != nil {
		t.Fatalf("Statement() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ID() != testID(102) || entry.JournalID() != testID(100) || entry.AccountID() != to {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Direction() != ledger.Credit || entry.Amount() != money.NewMoney(100, money.BRL) || !entry.CreatedAt().Equal(testTime) {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestLedgerAppend_SameAccountTwice_AppliesEveryEntry(t *testing.T) {
	_, repository, from, to := newLedgerFixture(t)
	first, _ := ledger.NewEntry(testID(101), from, ledger.Debit, money.NewMoney(100, money.BRL), testTime)
	second, _ := ledger.NewEntry(testID(102), from, ledger.Debit, money.NewMoney(50, money.BRL), testTime)
	third, _ := ledger.NewEntry(testID(103), to, ledger.Credit, money.NewMoney(150, money.BRL), testTime)
	journal, err := ledger.NewJournalTransaction(testID(100), []ledger.Entry{first, second, third})
	if err != nil {
		t.Fatalf("NewJournalTransaction() error = %v", err)
	}
	if err := repository.Append(context.Background(), journal); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	assertBalance(t, repository, from, -150)
	assertBalance(t, repository, to, 150)
}

func TestLedgerFindBalance_Missing_ReturnsNotFound(t *testing.T) {
	_, repository, _, _ := newLedgerFixture(t)
	_, err := repository.CalculateBalance(context.Background(), account.ID(testID(9)))
	if !errors.Is(err, account.ErrAccountNotFound) {
		t.Fatalf("CalculateBalance() error = %v, want %v", err, account.ErrAccountNotFound)
	}
}

func TestLedgerListStatement_Pages_NewestFirst(t *testing.T) {
	_, repository, from, to := newLedgerFixture(t)
	ctx := context.Background()
	for _, base := range []int{100, 200, 300} {
		appendTransfer(t, repository, base, from, to)
	}
	first, err := repository.ListStatement(ctx, to, page.Cursor{}, 2)
	if err != nil {
		t.Fatalf("Statement() error = %v", err)
	}
	if want := []string{testID(302), testID(202)}; !slices.Equal(entryIDs(first), want) {
		t.Fatalf("first page = %v, want %v", entryIDs(first), want)
	}
	second, err := repository.ListStatement(ctx, to, page.NewCursor(testID(202)), 2)
	if err != nil {
		t.Fatalf("Statement() error = %v", err)
	}
	if want := []string{testID(102)}; !slices.Equal(entryIDs(second), want) {
		t.Fatalf("second page = %v, want %v", entryIDs(second), want)
	}
}

func entryIDs(entries []ledger.Entry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID())
	}
	return ids
}

func TestLedgerListBalanceMismatches_Tampered_ReportsMismatch(t *testing.T) {
	pool, repository, from, to := newLedgerFixture(t)
	ctx := context.Background()
	appendTransfer(t, repository, 100, from, to)
	mismatches, accounts, err := repository.ListBalanceMismatches(ctx)
	if err != nil {
		t.Fatalf("CheckBalances() error = %v", err)
	}
	if len(mismatches) != 0 || accounts != 7 {
		t.Fatalf("CheckBalances() = %v, %d, want no mismatch and 7 accounts", mismatches, accounts)
	}
	if _, err := pool.Exec(ctx, "UPDATE accounts SET balance_minor = 1 WHERE id = $1", to); err != nil {
		t.Fatalf("tampering balance: %v", err)
	}
	mismatches, _, err = repository.ListBalanceMismatches(ctx)
	if err != nil {
		t.Fatalf("CheckBalances() error = %v", err)
	}
	if len(mismatches) != 1 || mismatches[0].AccountID != string(to) || mismatches[0].StoredMinor != 1 || mismatches[0].CalculatedMinor != 100 {
		t.Fatalf("mismatches = %+v", mismatches)
	}
}

func TestLedgerCalculateTotalsByCurrency_BalancedJournal_IsZero(t *testing.T) {
	_, repository, from, to := newLedgerFixture(t)
	appendTransfer(t, repository, 100, from, to)
	totals, err := repository.CalculateTotalsByCurrency(context.Background())
	if err != nil {
		t.Fatalf("SumByCurrency() error = %v", err)
	}
	if sum, found := totals["BRL"]; !found || sum != 0 || len(totals) != 1 {
		t.Fatalf("sums = %v, want only BRL at zero", totals)
	}
}

func TestLedgerRepository_ClosedPool_ReturnsErrors(t *testing.T) {
	pool, repository, from, _ := newLedgerFixture(t)
	ctx := context.Background()
	pool.Close()
	_, statementErr := repository.ListStatement(ctx, from, page.Cursor{}, 1)
	_, _, checkErr := repository.ListBalanceMismatches(ctx)
	_, totalsErr := repository.CalculateTotalsByCurrency(ctx)
	for name, err := range map[string]error{"Statement": statementErr, "CheckBalances": checkErr, "SumByCurrency": totalsErr} {
		if err == nil {
			t.Errorf("%s() error = nil, want an error", name)
		}
	}
}
