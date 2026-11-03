package account_test

import (
	"context"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
)

type updateCall struct {
	account         account.Account
	expectedVersion int64
}

type fakeRepository struct {
	accounts  map[account.ID]account.Account
	listed    []account.Account
	saved     []account.Account
	updates   []updateCall
	finds     int
	lists     int
	lastLimit page.Limit
	saveErr   error
	listErr   error
	updateErr error
}

func newFakeRepository(accounts ...account.Account) *fakeRepository {
	repository := &fakeRepository{accounts: map[account.ID]account.Account{}}
	for _, acc := range accounts {
		repository.accounts[acc.ID()] = acc
	}
	return repository
}

func (f *fakeRepository) Save(_ context.Context, acc account.Account) error {
	f.saved = append(f.saved, acc)
	return f.saveErr
}

func (f *fakeRepository) FindByID(_ context.Context, id account.ID) (account.Account, error) {
	f.finds++
	found, ok := f.accounts[id]
	if !ok {
		return account.Account{}, account.ErrAccountNotFound
	}
	return found, nil
}

func (f *fakeRepository) ListByOwner(_ context.Context, _ apikey.OwnerID, _ page.Cursor, limit page.Limit) ([]account.Account, error) {
	f.lists++
	f.lastLimit = limit
	return f.listed, f.listErr
}

func (f *fakeRepository) Update(_ context.Context, acc account.Account, expectedVersion int64) error {
	f.updates = append(f.updates, updateCall{account: acc, expectedVersion: expectedVersion})
	return f.updateErr
}

type fakeLedger struct {
	balance      money.Money
	entries      []ledger.Entry
	balances     int
	statements   int
	lastLimit    page.Limit
	balanceErr   error
	statementErr error
}

func (f *fakeLedger) Balance(context.Context, account.ID) (money.Money, error) {
	f.balances++
	return f.balance, f.balanceErr
}

func (f *fakeLedger) Statement(_ context.Context, _ account.ID, _ page.Cursor, limit page.Limit) ([]ledger.Entry, error) {
	f.statements++
	f.lastLimit = limit
	return f.entries, f.statementErr
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

type stubIDs struct {
	id string
}

func (s stubIDs) NewID() string {
	return s.id
}
