package account_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	service "github.com/UrielJaloto/paygo/internal/services/account"
)

const (
	ownerID      = "0190a000-0000-7000-8000-000000000001"
	otherOwnerID = "0190a000-0000-7000-8000-000000000002"
	accountID    = "0190a000-0000-7000-8000-00000000000a"
	secondID     = "0190a000-0000-7000-8000-00000000000b"
	thirdID      = "0190a000-0000-7000-8000-00000000000c"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	own     = service.AccountQuery{OwnerID: ownerID, AccountID: accountID}
)

func stored(id string, status account.Status) account.Account {
	return account.Restore(account.Snapshot{
		ID:        account.ID(id),
		OwnerID:   ownerID,
		Currency:  money.BRL,
		Kind:      account.KindCustomer,
		Status:    status,
		Version:   1,
		CreatedAt: now,
	})
}

func newService(repository *fakeRepository, reader *fakeLedger) *service.Service {
	return service.New(repository, reader, stubClock{now: now}, stubIDs{id: accountID})
}

func entry(t *testing.T, id string, direction ledger.Direction) ledger.Entry {
	t.Helper()
	created, err := ledger.NewEntry(id, accountID, direction, money.New(100, money.BRL), now)
	if err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return created.InJournal("t1")
}

func TestOpen_Valid_SavesAndReturnsOutput(t *testing.T) {
	repository := newFakeRepository()
	got, err := newService(repository, &fakeLedger{}).Open(context.Background(), service.OpenInput{OwnerID: ownerID, Currency: "BRL"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := service.AccountOutput{ID: accountID, OwnerID: ownerID, Currency: "BRL", Kind: "customer", Status: "active", Version: 1, CreatedAt: now}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(repository.saved) != 1 {
		t.Fatalf("saves = %d, want 1", len(repository.saved))
	}
}

func TestOpen_InvalidInput_ReturnsErrorWithoutSaving(t *testing.T) {
	cases := []struct {
		name  string
		input service.OpenInput
		want  error
	}{
		{name: "bad currency", input: service.OpenInput{OwnerID: ownerID, Currency: "GBP"}, want: money.ErrUnsupportedCurrency},
		{name: "bad owner", input: service.OpenInput{OwnerID: "x", Currency: "BRL"}, want: apikey.ErrInvalidOwner},
		{name: "system owner", input: service.OpenInput{OwnerID: string(apikey.SystemOwnerID), Currency: "BRL"}, want: apikey.ErrInvalidOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repository := newFakeRepository()
			_, err := newService(repository, &fakeLedger{}).Open(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if len(repository.saved) != 0 {
				t.Fatalf("saves = %d, want 0", len(repository.saved))
			}
		})
	}
}

func TestOpen_SaveFails_WrapsError(t *testing.T) {
	repository := newFakeRepository()
	repository.saveErr = errBoom
	_, err := newService(repository, &fakeLedger{}).Open(context.Background(), service.OpenInput{OwnerID: ownerID, Currency: "BRL"})
	if !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "opening account: ") {
		t.Fatalf("error = %v", err)
	}
}

func TestGet_OwnAccount_ReturnsOutput(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusActive))
	got, err := newService(repository, &fakeLedger{}).Get(context.Background(), own)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != accountID || got.Status != "active" {
		t.Fatalf("got %+v", got)
	}
}

func TestGet_Rejected_ReturnsExactError(t *testing.T) {
	cases := []struct {
		name      string
		query     service.AccountQuery
		want      error
		wantFinds int
	}{
		{name: "other owner", query: service.AccountQuery{OwnerID: otherOwnerID, AccountID: accountID}, want: account.ErrAccountNotFound, wantFinds: 1},
		{name: "missing", query: service.AccountQuery{OwnerID: ownerID, AccountID: secondID}, want: account.ErrAccountNotFound, wantFinds: 1},
		{name: "bad id", query: service.AccountQuery{OwnerID: ownerID, AccountID: "1"}, want: account.ErrInvalidAccountID},
		{name: "bad owner", query: service.AccountQuery{OwnerID: "x", AccountID: accountID}, want: apikey.ErrInvalidOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repository := newFakeRepository(stored(accountID, account.StatusActive))
			_, err := newService(repository, &fakeLedger{}).Get(context.Background(), tc.query)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if repository.finds != tc.wantFinds {
				t.Fatalf("finds = %d, want %d", repository.finds, tc.wantFinds)
			}
		})
	}
}

func TestList_Pagination(t *testing.T) {
	cases := []struct {
		name       string
		listed     []account.Account
		wantItems  int
		wantCursor string
	}{
		{
			name:       "first page",
			listed:     []account.Account{stored(accountID, account.StatusActive), stored(secondID, account.StatusActive), stored(thirdID, account.StatusActive)},
			wantItems:  2,
			wantCursor: page.NewCursor(secondID).Encode(),
		},
		{
			name:      "last page",
			listed:    []account.Account{stored(accountID, account.StatusActive), stored(secondID, account.StatusActive)},
			wantItems: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repository := newFakeRepository()
			repository.listed = tc.listed
			got, err := newService(repository, &fakeLedger{}).List(context.Background(), service.ListInput{OwnerID: ownerID, Limit: "2"})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got.Items) != tc.wantItems || got.NextCursor != tc.wantCursor || repository.lastLimit != 3 {
				t.Fatalf("got %+v, limit asked %d", got, repository.lastLimit)
			}
		})
	}
}

func TestList_Empty_ReturnsNonNilItems(t *testing.T) {
	got, err := newService(newFakeRepository(), &fakeLedger{}).List(context.Background(), service.ListInput{OwnerID: ownerID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.Items == nil || len(got.Items) != 0 || got.NextCursor != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestList_Rejected_ReturnsExactErrorWithoutListing(t *testing.T) {
	cases := []struct {
		name  string
		input service.ListInput
		want  error
	}{
		{name: "bad limit", input: service.ListInput{OwnerID: ownerID, Limit: "0"}, want: page.ErrInvalidPageLimit},
		{name: "bad cursor", input: service.ListInput{OwnerID: ownerID, Cursor: "!!"}, want: page.ErrInvalidCursor},
		{name: "bad owner", input: service.ListInput{OwnerID: "x"}, want: apikey.ErrInvalidOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repository := newFakeRepository()
			_, err := newService(repository, &fakeLedger{}).List(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if repository.lists != 0 {
				t.Fatalf("lists = %d, want 0", repository.lists)
			}
		})
	}
}

func TestList_RepositoryFails_WrapsError(t *testing.T) {
	repository := newFakeRepository()
	repository.listErr = errBoom
	_, err := newService(repository, &fakeLedger{}).List(context.Background(), service.ListInput{OwnerID: ownerID})
	if !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "listing accounts: ") {
		t.Fatalf("error = %v", err)
	}
}

func TestBalance_OwnAccount_ReturnsMaterializedBalance(t *testing.T) {
	reader := &fakeLedger{balance: money.New(300, money.BRL)}
	got, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Balance(context.Background(), own)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	want := service.BalanceOutput{AccountID: accountID, AmountMinor: 300, Currency: "BRL"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if reader.statements != 0 {
		t.Fatalf("statements = %d, want 0", reader.statements)
	}
}

func TestBalance_OtherOwner_DoesNotReadLedger(t *testing.T) {
	reader := &fakeLedger{}
	query := service.AccountQuery{OwnerID: otherOwnerID, AccountID: accountID}
	_, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Balance(context.Background(), query)
	if !errors.Is(err, account.ErrAccountNotFound) {
		t.Fatalf("error = %v", err)
	}
	if reader.balances != 0 {
		t.Fatalf("balances = %d, want 0", reader.balances)
	}
}

func TestBalance_ReaderFails_WrapsError(t *testing.T) {
	reader := &fakeLedger{balanceErr: errBoom}
	_, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Balance(context.Background(), own)
	if !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "reading balance: ") {
		t.Fatalf("error = %v", err)
	}
}

func TestStatement_KeepsRepositoryOrder(t *testing.T) {
	reader := &fakeLedger{entries: []ledger.Entry{entry(t, secondID, ledger.Credit), entry(t, accountID, ledger.Debit)}}
	input := service.PageInput{OwnerID: ownerID, AccountID: accountID}
	got, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Statement(context.Background(), input)
	if err != nil {
		t.Fatalf("Statement: %v", err)
	}
	want := service.StatementPage{Items: []service.StatementLine{
		{EntryID: secondID, TransferID: "t1", Direction: "credit", AmountMinor: 100, Currency: "BRL", CreatedAt: now},
		{EntryID: accountID, TransferID: "t1", Direction: "debit", AmountMinor: 100, Currency: "BRL", CreatedAt: now},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestStatement_MoreThanLimit_ReturnsNextCursor(t *testing.T) {
	reader := &fakeLedger{entries: []ledger.Entry{entry(t, thirdID, ledger.Credit), entry(t, secondID, ledger.Credit)}}
	input := service.PageInput{OwnerID: ownerID, AccountID: accountID, Limit: "1"}
	got, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Statement(context.Background(), input)
	if err != nil {
		t.Fatalf("Statement: %v", err)
	}
	if len(got.Items) != 1 || got.NextCursor != page.NewCursor(thirdID).Encode() || reader.lastLimit != 2 {
		t.Fatalf("got %+v, limit asked %d", got, reader.lastLimit)
	}
}

func TestStatement_Rejected_DoesNotReadLedger(t *testing.T) {
	cases := []struct {
		name  string
		input service.PageInput
		want  error
	}{
		{name: "bad cursor", input: service.PageInput{OwnerID: ownerID, AccountID: accountID, Cursor: "!!"}, want: page.ErrInvalidCursor},
		{name: "bad limit", input: service.PageInput{OwnerID: ownerID, AccountID: accountID, Limit: "abc"}, want: page.ErrInvalidPageLimit},
		{name: "other owner", input: service.PageInput{OwnerID: otherOwnerID, AccountID: accountID}, want: account.ErrAccountNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeLedger{}
			_, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Statement(context.Background(), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if reader.statements != 0 {
				t.Fatalf("statements = %d, want 0", reader.statements)
			}
		})
	}
}

func TestStatement_ReaderFails_WrapsError(t *testing.T) {
	reader := &fakeLedger{statementErr: errBoom}
	input := service.PageInput{OwnerID: ownerID, AccountID: accountID}
	_, err := newService(newFakeRepository(stored(accountID, account.StatusActive)), reader).Statement(context.Background(), input)
	if !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "reading statement: ") {
		t.Fatalf("error = %v", err)
	}
}

func TestFreeze_Active_UpdatesWithPreviousVersion(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusActive))
	got, err := newService(repository, &fakeLedger{}).Freeze(context.Background(), own)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if got.Status != "frozen" || got.Version != 2 {
		t.Fatalf("got %+v", got)
	}
	if len(repository.updates) != 1 || repository.updates[0].expectedVersion != 1 {
		t.Fatalf("updates = %+v", repository.updates)
	}
}

func TestUnfreeze_Frozen_UpdatesWithPreviousVersion(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusFrozen))
	got, err := newService(repository, &fakeLedger{}).Unfreeze(context.Background(), own)
	if err != nil {
		t.Fatalf("Unfreeze: %v", err)
	}
	if got.Status != "active" || len(repository.updates) != 1 || repository.updates[0].expectedVersion != 1 {
		t.Fatalf("got %+v, updates %+v", got, repository.updates)
	}
}

func TestFreeze_AlreadyFrozen_DoesNotUpdate(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusFrozen))
	_, err := newService(repository, &fakeLedger{}).Freeze(context.Background(), own)
	if !errors.Is(err, account.ErrAccountAlreadyFrozen) {
		t.Fatalf("error = %v", err)
	}
	if len(repository.updates) != 0 {
		t.Fatalf("updates = %d, want 0", len(repository.updates))
	}
}

func TestUnfreeze_Active_DoesNotUpdate(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusActive))
	_, err := newService(repository, &fakeLedger{}).Unfreeze(context.Background(), own)
	if !errors.Is(err, account.ErrAccountNotFrozen) {
		t.Fatalf("error = %v", err)
	}
	if len(repository.updates) != 0 {
		t.Fatalf("updates = %d, want 0", len(repository.updates))
	}
}

func TestFreeze_StaleVersion_ReturnsConcurrentModification(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusActive))
	repository.updateErr = account.ErrConcurrentModification
	_, err := newService(repository, &fakeLedger{}).Freeze(context.Background(), own)
	if !errors.Is(err, account.ErrConcurrentModification) {
		t.Fatalf("error = %v", err)
	}
}

func TestFreeze_NotOwned_DoesNotUpdate(t *testing.T) {
	repository := newFakeRepository(stored(accountID, account.StatusActive))
	query := service.AccountQuery{OwnerID: otherOwnerID, AccountID: accountID}
	_, err := newService(repository, &fakeLedger{}).Freeze(context.Background(), query)
	if !errors.Is(err, account.ErrAccountNotFound) || len(repository.updates) != 0 {
		t.Fatalf("error = %v, updates = %d", err, len(repository.updates))
	}
}
