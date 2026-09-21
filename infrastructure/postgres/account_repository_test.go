//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

const testOwner apikey.OwnerID = "01900000-0000-7000-8000-00000000f001"

var testTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func testID(sequence int) string {
	return fmt.Sprintf("01900000-0000-7000-8000-%012x", sequence+1000)
}

func saveCustomer(t *testing.T, repository *postgres.AccountRepository, sequence int, owner apikey.OwnerID) account.Account {
	t.Helper()
	customer, err := account.NewCustomer(account.ID(testID(sequence)), owner, money.BRL, testTime)
	if err != nil {
		t.Fatalf("NewCustomer() error = %v", err)
	}
	if err := repository.Save(context.Background(), customer); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return customer
}

func accountIDs(accounts []account.Account) []account.ID {
	ids := make([]account.ID, 0, len(accounts))
	for _, found := range accounts {
		ids = append(ids, found.ID())
	}
	return ids
}

func TestAccountSave_ThenFind_RoundTrips(t *testing.T) {
	repository := postgres.NewAccountRepository(newTestPool(t))
	saved := saveCustomer(t, repository, 1, testOwner)

	found, err := repository.FindByID(context.Background(), saved.ID())

	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.ID() != saved.ID() || found.OwnerID() != testOwner || found.Currency() != money.BRL {
		t.Fatalf("found = %+v, want %+v", found, saved)
	}
	if found.Kind() != account.KindCustomer || found.Status() != account.StatusActive || found.Version() != 1 {
		t.Fatalf("found = %+v, want %+v", found, saved)
	}
	if !found.CreatedAt().Equal(testTime) {
		t.Fatalf("CreatedAt = %v, want %v", found.CreatedAt(), testTime)
	}
}

func TestAccountFindByID_Missing_ReturnsNotFound(t *testing.T) {
	repository := postgres.NewAccountRepository(newTestPool(t))
	_, err := repository.FindByID(context.Background(), account.ID(testID(1)))
	if !errors.Is(err, account.ErrAccountNotFound) {
		t.Fatalf("FindByID() error = %v, want %v", err, account.ErrAccountNotFound)
	}
}

func TestAccountListByOwner_Paging_ReturnsPagesInOrder(t *testing.T) {
	repository := postgres.NewAccountRepository(newTestPool(t))
	ctx := context.Background()
	for sequence := 1; sequence <= 5; sequence++ {
		saveCustomer(t, repository, sequence, testOwner)
	}
	first, err := repository.ListByOwner(ctx, testOwner, page.Cursor{}, 2)
	if err != nil {
		t.Fatalf("ListByOwner() error = %v", err)
	}
	if want := []account.ID{account.ID(testID(1)), account.ID(testID(2))}; !slices.Equal(accountIDs(first), want) {
		t.Fatalf("first page = %v, want %v", accountIDs(first), want)
	}
	second, err := repository.ListByOwner(ctx, testOwner, page.NewCursor(testID(2)), 2)
	if err != nil {
		t.Fatalf("ListByOwner() error = %v", err)
	}
	if want := []account.ID{account.ID(testID(3)), account.ID(testID(4))}; !slices.Equal(accountIDs(second), want) {
		t.Fatalf("second page = %v, want %v", accountIDs(second), want)
	}
}

func TestAccountListByOwner_OtherOwnerAndSettlement_AreExcluded(t *testing.T) {
	repository := postgres.NewAccountRepository(newTestPool(t))
	ctx := context.Background()
	saveCustomer(t, repository, 1, "01900000-0000-7000-8000-00000000f002")
	for _, owner := range []apikey.OwnerID{testOwner, apikey.SystemOwnerID} {
		listed, err := repository.ListByOwner(ctx, owner, page.Cursor{}, 10)
		if err != nil {
			t.Fatalf("ListByOwner() error = %v", err)
		}
		if len(listed) != 0 {
			t.Fatalf("listed = %v, want none", accountIDs(listed))
		}
	}
}

func TestAccountUpdate_ExpectedVersion_PersistsOrRejects(t *testing.T) {
	repository := postgres.NewAccountRepository(newTestPool(t))
	ctx := context.Background()
	stored := saveCustomer(t, repository, 1, testOwner)
	if err := stored.Freeze(); err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}
	if err := repository.Update(ctx, stored, 1); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	found, err := repository.FindByID(ctx, stored.ID())
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Version() != 2 || found.Status() != account.StatusFrozen {
		t.Fatalf("found = %+v, want version 2 and frozen", found)
	}
	if err := repository.Update(ctx, stored, 1); !errors.Is(err, account.ErrConcurrentModification) {
		t.Fatalf("Update() error = %v, want %v", err, account.ErrConcurrentModification)
	}
}

func TestAccountLockByIDs_Ids_LockInOrderOrReportMissing(t *testing.T) {
	pool := newTestPool(t)
	repository := postgres.NewAccountRepository(pool)
	ctx := context.Background()
	first := saveCustomer(t, repository, 1, testOwner)
	second := saveCustomer(t, repository, 2, testOwner)

	locked, err := repository.LockByIDs(ctx, []account.ID{second.ID(), first.ID()})
	if err != nil {
		t.Fatalf("LockByIDs() error = %v", err)
	}
	if want := []account.ID{first.ID(), second.ID()}; !slices.Equal(accountIDs(locked), want) {
		t.Fatalf("locked = %v, want %v", accountIDs(locked), want)
	}
	_, err = repository.LockByIDs(ctx, []account.ID{first.ID(), account.ID(testID(9))})
	if !errors.Is(err, account.ErrAccountNotFound) {
		t.Fatalf("LockByIDs() error = %v, want %v", err, account.ErrAccountNotFound)
	}
}

func TestAccountRepository_ClosedPool_ReturnsErrors(t *testing.T) {
	pool := newTestPool(t)
	repository := postgres.NewAccountRepository(pool)
	ctx := context.Background()
	customer := saveCustomer(t, repository, 1, testOwner)
	pool.Close()
	_, listErr := repository.ListByOwner(ctx, testOwner, page.Cursor{}, 1)
	_, lockErr := repository.LockByIDs(ctx, []account.ID{customer.ID()})
	for name, err := range map[string]error{
		"Save":        repository.Save(ctx, customer),
		"Update":      repository.Update(ctx, customer, 1),
		"ListByOwner": listErr,
		"LockByIDs":   lockErr,
	} {
		if err == nil {
			t.Errorf("%s() error = nil, want an error", name)
		}
	}
}
