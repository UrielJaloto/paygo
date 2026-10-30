package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	accountservice "github.com/UrielJaloto/paygo/services/account"
	transferservice "github.com/UrielJaloto/paygo/services/transfer"
)

const (
	OwnerA apikey.OwnerID = "01900000-0000-7000-8000-00000000f001"
	OwnerB apikey.OwnerID = "01900000-0000-7000-8000-00000000f002"

	SettlementBRL account.ID = "01900000-0000-7000-8000-000000000001"
)

var At = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type AccountStore interface {
	accountservice.Repository
	transferservice.AccountLocker
}

type LedgerStore interface {
	accountservice.LedgerReader
	Append(ctx context.Context, journal ledger.JournalTransaction) error
}

type TransferStore interface {
	transferservice.Repository
	transferservice.Reader
}

type Stores struct {
	Accounts  AccountStore
	Ledger    LedgerStore
	Transfers TransferStore
	Unit      transferservice.UnitOfWork
	Outbox    OutboxStore
}

type contractCase struct {
	name string
	run  func(t *testing.T, stores Stores)
}

func runCases(t *testing.T, newStores func(t *testing.T) Stores, cases []contractCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newStores(t))
		})
	}
}

func RunAccountContract(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Helper()
	runCases(t, newStores, []contractCase{
		{"save then find round trips", accountRoundTrip},
		{"find missing is not found", accountMissing},
		{"list pages in id order", accountPages},
		{"list excludes other owners and system accounts", accountListScope},
		{"update checks the expected version", accountUpdate},
		{"lock returns accounts in id order", accountLockOrder},
		{"lock with a missing id is not found", accountLockMissing},
	})
}

func ID(sequence int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-%012x", sequence+1000)
}

func SaveCustomer(t *testing.T, stores Stores, sequence int) account.Account {
	t.Helper()
	customer := newCustomer(t, sequence, OwnerA)
	NoError(t, stores.Accounts.Save(context.Background(), customer))
	return customer
}

func newCustomer(t *testing.T, sequence int, owner apikey.OwnerID) account.Account {
	t.Helper()
	customer, err := account.NewCustomer(account.ID(ID(sequence)), owner, money.BRL, At)
	NoError(t, err)
	return customer
}

func accountIDs(accounts []account.Account) []account.ID {
	ids := make([]account.ID, 0, len(accounts))
	for _, found := range accounts {
		ids = append(ids, found.ID())
	}
	return ids
}

func accountRoundTrip(t *testing.T, stores Stores) {
	saved := SaveCustomer(t, stores, 1)
	found, err := stores.Accounts.FindByID(context.Background(), saved.ID())
	NoError(t, err)
	Equal(t, found.ID(), saved.ID())
	Equal(t, found.OwnerID(), saved.OwnerID())
	Equal(t, found.Currency(), saved.Currency())
	Equal(t, found.Kind(), saved.Kind())
	Equal(t, found.Status(), saved.Status())
	Equal(t, found.Version(), saved.Version())
	Equal(t, found.CreatedAt().Equal(saved.CreatedAt()), true)
}

func accountMissing(t *testing.T, stores Stores) {
	_, err := stores.Accounts.FindByID(context.Background(), account.ID(ID(99)))
	ErrorIs(t, err, account.ErrAccountNotFound)
}

func accountPages(t *testing.T, stores Stores) {
	ctx := context.Background()
	first := SaveCustomer(t, stores, 1)
	second := SaveCustomer(t, stores, 2)
	third := SaveCustomer(t, stores, 3)
	opening, err := stores.Accounts.ListByOwner(ctx, OwnerA, page.Cursor{}, 2)
	NoError(t, err)
	EqualSlices(t, accountIDs(opening), []account.ID{first.ID(), second.ID()})
	rest, err := stores.Accounts.ListByOwner(ctx, OwnerA, page.NewCursor(string(second.ID())), 2)
	NoError(t, err)
	EqualSlices(t, accountIDs(rest), []account.ID{third.ID()})
}

func accountListScope(t *testing.T, stores Stores) {
	mine := SaveCustomer(t, stores, 1)
	NoError(t, stores.Accounts.Save(context.Background(), newCustomer(t, 2, OwnerB)))
	listed, err := stores.Accounts.ListByOwner(context.Background(), OwnerA, page.Cursor{}, 10)
	NoError(t, err)
	EqualSlices(t, accountIDs(listed), []account.ID{mine.ID()})
	system, err := stores.Accounts.ListByOwner(context.Background(), apikey.SystemOwnerID, page.Cursor{}, 10)
	NoError(t, err)
	Equal(t, len(system), 0)
}

func accountUpdate(t *testing.T, stores Stores) {
	ctx := context.Background()
	frozen := SaveCustomer(t, stores, 1)
	NoError(t, frozen.Freeze())
	NoError(t, stores.Accounts.Update(ctx, frozen, frozen.Version()-1))
	found, err := stores.Accounts.FindByID(ctx, frozen.ID())
	NoError(t, err)
	Equal(t, found.Status(), account.StatusFrozen)
	Equal(t, found.Version(), frozen.Version())
	ErrorIs(t, stores.Accounts.Update(ctx, frozen, frozen.Version()-1), account.ErrConcurrentModification)
}

func accountLockOrder(t *testing.T, stores Stores) {
	first := SaveCustomer(t, stores, 1)
	second := SaveCustomer(t, stores, 2)
	locked, err := stores.Accounts.LockByIDs(context.Background(), []account.ID{second.ID(), first.ID()})
	NoError(t, err)
	EqualSlices(t, accountIDs(locked), []account.ID{first.ID(), second.ID()})
}

func accountLockMissing(t *testing.T, stores Stores) {
	first := SaveCustomer(t, stores, 1)
	_, err := stores.Accounts.LockByIDs(context.Background(), []account.ID{first.ID(), account.ID(ID(99))})
	ErrorIs(t, err, account.ErrAccountNotFound)
}
