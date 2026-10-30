//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	accountservice "github.com/UrielJaloto/paygo/internal/services/account"
	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
)

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
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.run(t, newStores(t))
		})
	}
}

func runAccountContract(t *testing.T, newStores func(t *testing.T) Stores) {
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

func storeCustomer(t *testing.T, stores Stores, sequence int) account.Account {
	t.Helper()
	customer := newCustomer(t, sequence, testOwner)
	noError(t, stores.Accounts.Save(context.Background(), customer))
	return customer
}

func newCustomer(t *testing.T, sequence int, owner apikey.OwnerID) account.Account {
	t.Helper()
	customer, err := account.NewCustomer(account.ID(testID(sequence)), owner, money.BRL, testTime)
	noError(t, err)
	return customer
}

func accountRoundTrip(t *testing.T, stores Stores) {
	saved := storeCustomer(t, stores, 1)
	found, err := stores.Accounts.FindByID(context.Background(), saved.ID())
	noError(t, err)
	equal(t, found.ID(), saved.ID())
	equal(t, found.OwnerID(), saved.OwnerID())
	equal(t, found.Currency(), saved.Currency())
	equal(t, found.Kind(), saved.Kind())
	equal(t, found.Status(), saved.Status())
	equal(t, found.Version(), saved.Version())
	equal(t, found.CreatedAt().Equal(saved.CreatedAt()), true)
}

func accountMissing(t *testing.T, stores Stores) {
	_, err := stores.Accounts.FindByID(context.Background(), account.ID(testID(99)))
	errorIs(t, err, account.ErrAccountNotFound)
}

func accountPages(t *testing.T, stores Stores) {
	ctx := context.Background()
	first := storeCustomer(t, stores, 1)
	second := storeCustomer(t, stores, 2)
	third := storeCustomer(t, stores, 3)
	opening, err := stores.Accounts.ListByOwner(ctx, testOwner, page.Cursor{}, 2)
	noError(t, err)
	equalSlices(t, accountIDs(opening), []account.ID{first.ID(), second.ID()})
	rest, err := stores.Accounts.ListByOwner(ctx, testOwner, page.NewCursor(string(second.ID())), 2)
	noError(t, err)
	equalSlices(t, accountIDs(rest), []account.ID{third.ID()})
}

func accountListScope(t *testing.T, stores Stores) {
	mine := storeCustomer(t, stores, 1)
	noError(t, stores.Accounts.Save(context.Background(), newCustomer(t, 2, otherOwner)))
	listed, err := stores.Accounts.ListByOwner(context.Background(), testOwner, page.Cursor{}, 10)
	noError(t, err)
	equalSlices(t, accountIDs(listed), []account.ID{mine.ID()})
	system, err := stores.Accounts.ListByOwner(context.Background(), apikey.SystemOwnerID, page.Cursor{}, 10)
	noError(t, err)
	equal(t, len(system), 0)
}

func accountUpdate(t *testing.T, stores Stores) {
	ctx := context.Background()
	frozen := storeCustomer(t, stores, 1)
	noError(t, frozen.Freeze())
	noError(t, stores.Accounts.Update(ctx, frozen, frozen.Version()-1))
	found, err := stores.Accounts.FindByID(ctx, frozen.ID())
	noError(t, err)
	equal(t, found.Status(), account.StatusFrozen)
	equal(t, found.Version(), frozen.Version())
	errorIs(t, stores.Accounts.Update(ctx, frozen, frozen.Version()-1), account.ErrConcurrentModification)
}

func accountLockOrder(t *testing.T, stores Stores) {
	first := storeCustomer(t, stores, 1)
	second := storeCustomer(t, stores, 2)
	locked, err := stores.Accounts.LockByIDs(context.Background(), []account.ID{second.ID(), first.ID()})
	noError(t, err)
	equalSlices(t, accountIDs(locked), []account.ID{first.ID(), second.ID()})
}

func accountLockMissing(t *testing.T, stores Stores) {
	first := storeCustomer(t, stores, 1)
	_, err := stores.Accounts.LockByIDs(context.Background(), []account.ID{first.ID(), account.ID(testID(99))})
	errorIs(t, err, account.ErrAccountNotFound)
}
