package ledger_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
)

const (
	accountA account.ID = "a"
	accountB account.ID = "b"
)

var postedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func entry(t *testing.T, direction ledger.Direction, minor int64, currency money.Currency) ledger.Entry {
	t.Helper()
	created, err := ledger.NewEntry("entry", accountA, direction, money.New(minor, currency), postedAt)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return created
}

func TestNewEntry_ExposesFields(t *testing.T) {
	created, err := ledger.NewEntry("e1", accountA, ledger.Debit, money.New(100, money.BRL), postedAt)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	if created.ID() != "e1" || created.AccountID() != accountA || created.Direction() != ledger.Debit {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Amount() != money.New(100, money.BRL) || !created.CreatedAt().Equal(postedAt) {
		t.Fatalf("unexpected values: %+v", created)
	}
}

func TestNewEntry_NotPositive_ReturnsErrInvalidAmount(t *testing.T) {
	_, err := ledger.NewEntry("e1", accountA, ledger.Debit, money.New(0, money.BRL), postedAt)
	if !errors.Is(err, money.ErrInvalidAmount) {
		t.Fatalf("error = %v, want ErrInvalidAmount", err)
	}
}

func TestNewJournalTransaction_ValidatesEntries(t *testing.T) {
	cases := []struct {
		name    string
		entries []ledger.Entry
		wantErr error
	}{
		{name: "one entry", entries: []ledger.Entry{entry(t, ledger.Credit, 100, money.BRL)}, wantErr: ledger.ErrTooFewEntries},
		{name: "balanced", entries: []ledger.Entry{entry(t, ledger.Debit, 100, money.BRL), entry(t, ledger.Credit, 100, money.BRL)}},
		{name: "unbalanced", entries: []ledger.Entry{entry(t, ledger.Debit, 100, money.BRL), entry(t, ledger.Credit, 99, money.BRL)}, wantErr: ledger.ErrUnbalancedTransaction},
		{name: "mixed currency", entries: []ledger.Entry{entry(t, ledger.Debit, 100, money.BRL), entry(t, ledger.Credit, 100, money.USD)}, wantErr: money.ErrCurrencyMismatch},
		{name: "three entries", entries: []ledger.Entry{entry(t, ledger.Debit, 100, money.BRL), entry(t, ledger.Credit, 60, money.BRL), entry(t, ledger.Credit, 40, money.BRL)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ledger.NewJournalTransaction("j1", tc.entries); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestJournalTransaction_CopiesEntries(t *testing.T) {
	entries := []ledger.Entry{entry(t, ledger.Debit, 100, money.BRL), entry(t, ledger.Credit, 100, money.BRL)}
	journal, err := ledger.NewJournalTransaction("j1", entries)
	if err != nil {
		t.Fatalf("NewJournalTransaction: %v", err)
	}
	entries[0] = entry(t, ledger.Credit, 1, money.BRL)
	returned := journal.Entries()
	returned[1] = entry(t, ledger.Debit, 1, money.BRL)
	got := journal.Entries()
	if got[0].Direction() != ledger.Debit || got[1].Direction() != ledger.Credit {
		t.Fatalf("journal changed: %+v", got)
	}
}

func TestBalance_SumsCreditsMinusDebits(t *testing.T) {
	cases := []struct {
		name    string
		entries []ledger.Entry
		want    int64
	}{
		{name: "empty"},
		{name: "mixed", entries: []ledger.Entry{entry(t, ledger.Credit, 500, money.BRL), entry(t, ledger.Debit, 200, money.BRL)}, want: 300},
		{name: "negative", entries: []ledger.Entry{entry(t, ledger.Debit, 1, money.BRL)}, want: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ledger.Balance(money.BRL, tc.entries)
			if err != nil || got != money.New(tc.want, money.BRL) {
				t.Fatalf("got (%s, %v), want %d", got, err, tc.want)
			}
		})
	}
}

func TestBalance_Failures_ReturnExactError(t *testing.T) {
	cases := []struct {
		name    string
		entries []ledger.Entry
		wantErr error
	}{
		{name: "wrong currency", entries: []ledger.Entry{entry(t, ledger.Credit, 1, money.USD)}, wantErr: money.ErrCurrencyMismatch},
		{name: "overflow", entries: []ledger.Entry{entry(t, ledger.Credit, math.MaxInt64, money.BRL), entry(t, ledger.Credit, 1, money.BRL)}, wantErr: money.ErrAmountOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ledger.Balance(money.BRL, tc.entries); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTransfer_BuildsDebitThenCredit(t *testing.T) {
	journal, err := ledger.Transfer(ledger.Posting{
		ID:       "j1",
		EntryIDs: [2]string{"e1", "e2"},
		From:     accountA,
		To:       accountB,
		Amount:   money.New(100, money.BRL),
		At:       postedAt,
	})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	got := journal.Entries()
	if journal.ID() != "j1" || len(got) != 2 {
		t.Fatalf("got %+v", journal)
	}
	if got[0].ID() != "e1" || got[0].AccountID() != accountA || got[0].Direction() != ledger.Debit {
		t.Fatalf("debit = %+v", got[0])
	}
	if got[1].ID() != "e2" || got[1].AccountID() != accountB || got[1].Direction() != ledger.Credit {
		t.Fatalf("credit = %+v", got[1])
	}
}

func TestTransfer_InvalidAmount_ReturnsErrInvalidAmount(t *testing.T) {
	_, err := ledger.Transfer(ledger.Posting{Amount: money.New(0, money.BRL)})
	if !errors.Is(err, money.ErrInvalidAmount) {
		t.Fatalf("error = %v, want ErrInvalidAmount", err)
	}
}
