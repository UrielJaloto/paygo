package transfer_test

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
	service "github.com/UrielJaloto/paygo/services/transfer"
)

type savedTransfer struct {
	transfer    transfer.Transfer
	fingerprint transfer.Fingerprint
}

type fakeStore struct {
	existingAfterLock *savedTransfer
	failOnCall        int
	accounts          map[account.ID]account.Account
	balances          map[account.ID]money.Money
	existing          *savedTransfer
	originals         map[transfer.ID]transfer.Transfer
	locked            []account.ID
	journals          []ledger.JournalTransaction
	saved             []savedTransfer
	events            []event.Event
	marked            []transfer.Transfer
	calls             []string
	failures              map[string]error
}

func newFakeStore(accounts ...account.Account) *fakeStore {
	store := &fakeStore{
		accounts:  map[account.ID]account.Account{},
		balances:  map[account.ID]money.Money{},
		originals: map[transfer.ID]transfer.Transfer{},
		failures:      map[string]error{},
	}
	for _, acc := range accounts {
		store.accounts[acc.ID()] = acc
	}
	return store
}

func (f *fakeStore) record(call string) error {
	f.calls = append(f.calls, call)
	if len(f.calls) == f.failOnCall {
		return errBoom
	}
	return f.failures[call]
}

func (f *fakeStore) LockByIDs(_ context.Context, ids []account.ID) ([]account.Account, error) {
	f.locked = ids
	if err := f.record("lock"); err != nil {
		return nil, err
	}
	if f.existingAfterLock != nil {
		f.existing = f.existingAfterLock
	}
	locked := make([]account.Account, 0, len(ids))
	for _, id := range ids {
		found, ok := f.accounts[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", account.ErrAccountNotFound, id)
		}
		locked = append(locked, found)
	}
	return locked, nil
}

func (f *fakeStore) CalculateBalance(_ context.Context, id account.ID) (money.Money, error) {
	return f.balances[id], f.record("balance")
}

func (f *fakeStore) Append(_ context.Context, journal ledger.JournalTransaction) error {
	f.journals = append(f.journals, journal)
	return f.record("append")
}

func (f *fakeStore) FindByIdempotencyKey(context.Context, apikey.OwnerID, transfer.IdempotencyKey) (transfer.Transfer, transfer.Fingerprint, error) {
	if err := f.record("find"); err != nil {
		return transfer.Transfer{}, "", err
	}
	if f.existing == nil {
		return transfer.Transfer{}, "", transfer.ErrTransferNotFound
	}
	return f.existing.transfer, f.existing.fingerprint, nil
}

func (f *fakeStore) FindByIDForUpdate(_ context.Context, id transfer.ID) (transfer.Transfer, error) {
	if err := f.record("original"); err != nil {
		return transfer.Transfer{}, err
	}
	found, ok := f.originals[id]
	if !ok {
		return transfer.Transfer{}, transfer.ErrTransferNotFound
	}
	return found, nil
}

func (f *fakeStore) Save(_ context.Context, t transfer.Transfer, fingerprint transfer.Fingerprint) error {
	f.saved = append(f.saved, savedTransfer{transfer: t, fingerprint: fingerprint})
	return f.record("save")
}

func (f *fakeStore) MarkReversed(_ context.Context, t transfer.Transfer) error {
	f.marked = append(f.marked, t)
	return f.record("mark")
}

func (f *fakeStore) Add(_ context.Context, e event.Event) error {
	f.events = append(f.events, e)
	return f.record("outbox")
}

func (f *fakeStore) wasCalled(call string) bool {
	return slices.Contains(f.calls, call)
}

type fakeUnitOfWork struct {
	store *fakeStore
	runs  int
	err   error
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store service.Store) error) error {
	f.runs++
	if f.err != nil {
		return f.err
	}
	return work(ctx, service.Store{Accounts: f.store, Ledger: f.store, Transfers: f.store, Outbox: f.store})
}

type fakeReader struct {
	found   map[transfer.ID]transfer.Transfer
	listed  []transfer.Transfer
	limit   page.Limit
	findErr error
	listErr error
}

func (f *fakeReader) FindByID(_ context.Context, id transfer.ID) (transfer.Transfer, error) {
	if f.findErr != nil {
		return transfer.Transfer{}, f.findErr
	}
	found, ok := f.found[id]
	if !ok {
		return transfer.Transfer{}, transfer.ErrTransferNotFound
	}
	return found, nil
}

func (f *fakeReader) ListByAccount(_ context.Context, _ account.ID, _ page.Cursor, limit page.Limit) ([]transfer.Transfer, error) {
	f.limit = limit
	return f.listed, f.listErr
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

type sequenceIDs struct {
	next int
}

func (s *sequenceIDs) NewID() string {
	s.next++
	return fmt.Sprintf("id-%d", s.next)
}
