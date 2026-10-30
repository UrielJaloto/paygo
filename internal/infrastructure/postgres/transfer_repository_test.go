//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
)

const (
	testFingerprint transfer.Fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherOwner      apikey.OwnerID       = "01900000-0000-7000-8000-00000000f002"
	settlementBRL   account.ID           = "01900000-0000-7000-8000-000000000001"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return testTime }

type sequentialIDs struct{ next atomic.Int64 }

func (s *sequentialIDs) NewID() string { return testID(int(s.next.Add(1)) + 10000) }

type transferFixture struct {
	pool       *pgxpool.Pool
	repository *postgres.TransferRepository
	from       account.ID
	to         account.ID
}

func newTransferFixture(t *testing.T) transferFixture {
	t.Helper()
	pool := newTestPool(t)
	accounts := postgres.NewAccountRepository(pool)
	return transferFixture{
		pool:       pool,
		repository: postgres.NewTransferRepository(pool),
		from:       saveCustomer(t, accounts, 1, testOwner).ID(),
		to:         saveCustomer(t, accounts, 2, testOwner).ID(),
	}
}

func (f transferFixture) newTransfer(t *testing.T, sequence int, owner apikey.OwnerID, key transfer.IdempotencyKey) transfer.Transfer {
	t.Helper()
	created, err := transfer.NewMoney(transfer.ID(testID(sequence)), transfer.Request{
		OwnerID: owner,
		Key:     key,
		Kind:    transfer.KindTransfer,
		From:    f.from,
		To:      f.to,
		Amount:  money.NewMoney(100, money.BRL),
	}, testTime)
	if err != nil {
		t.Fatalf("transfer.NewMoney() error = %v", err)
	}
	return created
}

func (f transferFixture) save(t *testing.T, sequence int, key transfer.IdempotencyKey) transfer.Transfer {
	t.Helper()
	created := f.newTransfer(t, sequence, testOwner, key)
	if err := f.repository.Save(context.Background(), created, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return created
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var rows int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return rows
}

func TestPosting_BuildJournalSave_ThenFindByKey_RoundTrips(t *testing.T) {
	fixture := newTransferFixture(t)
	saved := fixture.save(t, 100, "key-1")

	found, fingerprint, err := fixture.repository.FindByIdempotencyKey(context.Background(), testOwner, "key-1")

	if err != nil {
		t.Fatalf("FindByIdempotencyKey() error = %v", err)
	}
	if fingerprint != testFingerprint {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, testFingerprint)
	}
	assertChecks(t, map[string]bool{
		"id":          found.ID() == saved.ID(),
		"owner":       found.OwnerID() == testOwner,
		"key":         found.IdempotencyKey() == "key-1",
		"kind":        found.Kind() == transfer.KindTransfer,
		"from":        found.From() == fixture.from,
		"to":          found.To() == fixture.to,
		"amount":      found.Amount() == saved.Amount(),
		"status":      found.Status() == transfer.StatusCompleted,
		"reversal of": found.ReversalOf() == "",
		"created at":  found.CreatedAt().Equal(testTime),
	})
}

func assertChecks(t *testing.T, checks map[string]bool) {
	t.Helper()
	for name, passed := range checks {
		if !passed {
			t.Errorf("%s does not match", name)
		}
	}
}

func TestPosting_BuildJournalFind_Missing_ReturnsNotFound(t *testing.T) {
	fixture := newTransferFixture(t)
	ctx := context.Background()
	_, _, keyErr := fixture.repository.FindByIdempotencyKey(ctx, testOwner, "missing")
	_, idErr := fixture.repository.FindByID(ctx, transfer.ID(testID(9)))
	_, lockErr := fixture.repository.FindByIDForUpdate(ctx, transfer.ID(testID(9)))
	for name, err := range map[string]error{"FindByIdempotencyKey": keyErr, "FindByID": idErr, "FindByIDForUpdate": lockErr} {
		if !errors.Is(err, transfer.ErrTransferNotFound) {
			t.Errorf("%s() error = %v, want %v", name, err, transfer.ErrTransferNotFound)
		}
	}
}

func TestPosting_BuildJournalSave_DuplicateKey_ConflictsPerOwner(t *testing.T) {
	fixture := newTransferFixture(t)
	ctx := context.Background()
	fixture.save(t, 100, "key-1")
	err := fixture.repository.Save(ctx, fixture.newTransfer(t, 101, testOwner, "key-1"), testFingerprint)
	if !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("Save() error = %v, want %v", err, transfer.ErrIdempotencyConflict)
	}
	if err := fixture.repository.Save(ctx, fixture.newTransfer(t, 102, otherOwner, "key-1"), testFingerprint); err != nil {
		t.Fatalf("Save() for another owner error = %v", err)
	}
}

func TestPosting_BuildJournalMarkReversed_Existing_ChangesStatus(t *testing.T) {
	fixture := newTransferFixture(t)
	ctx := context.Background()
	saved := fixture.save(t, 100, "key-1")
	if err := saved.Reverse(); err != nil {
		t.Fatalf("Reverse() error = %v", err)
	}
	if err := fixture.repository.MarkReversed(ctx, saved); err != nil {
		t.Fatalf("MarkReversed() error = %v", err)
	}
	for name, find := range map[string]func(context.Context, transfer.ID) (transfer.Transfer, error){
		"FindByID":          fixture.repository.FindByID,
		"FindByIDForUpdate": fixture.repository.FindByIDForUpdate,
	} {
		found, err := find(ctx, saved.ID())
		if err != nil {
			t.Fatalf("%s() error = %v", name, err)
		}
		if found.Status() != transfer.StatusReversed {
			t.Fatalf("%s() status = %q, want %q", name, found.Status(), transfer.StatusReversed)
		}
	}
}

func TestPosting_BuildJournalListByAccount_Pages_NewestFirst(t *testing.T) {
	fixture := newTransferFixture(t)
	ctx := context.Background()
	for sequence, key := range map[int]transfer.IdempotencyKey{100: "key-1", 101: "key-2", 102: "key-3"} {
		fixture.save(t, sequence, key)
	}
	first, err := fixture.repository.ListByAccount(ctx, fixture.to, page.Cursor{}, 2)
	if err != nil {
		t.Fatalf("ListByAccount() error = %v", err)
	}
	if want := []transfer.ID{transfer.ID(testID(102)), transfer.ID(testID(101))}; !slices.Equal(transferIDs(first), want) {
		t.Fatalf("first page = %v, want %v", transferIDs(first), want)
	}
	second, err := fixture.repository.ListByAccount(ctx, fixture.from, page.NewCursor(testID(101)), 2)
	if err != nil {
		t.Fatalf("ListByAccount() error = %v", err)
	}
	if want := []transfer.ID{transfer.ID(testID(100))}; !slices.Equal(transferIDs(second), want) {
		t.Fatalf("second page = %v, want %v", transferIDs(second), want)
	}
}

func transferIDs(transfers []transfer.Transfer) []transfer.ID {
	ids := make([]transfer.ID, 0, len(transfers))
	for _, found := range transfers {
		ids = append(ids, found.ID())
	}
	return ids
}

func TestOutboxAdd_Event_StoresOwnerIDs(t *testing.T) {
	fixture := newTransferFixture(t)
	ctx := context.Background()
	saved := fixture.save(t, 100, "key-1")
	metadata := event.Metadata{ID: event.ID(testID(200)), At: testTime}
	err := postgres.NewTransferUnitOfWork(fixture.pool).Run(ctx, func(ctx context.Context, store transferservice.Store) error {
		return store.Outbox.Add(ctx, event.NewTransferEvent(metadata, saved, []apikey.OwnerID{testOwner, otherOwner}))
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	var owners []string
	var eventType, transferID string
	row := fixture.pool.QueryRow(ctx, "SELECT owner_ids::text[], type, transfer_id::text FROM outbox_events WHERE id = $1", testID(200))
	if err := row.Scan(&owners, &eventType, &transferID); err != nil {
		t.Fatalf("reading outbox row: %v", err)
	}
	if want := []string{string(testOwner), string(otherOwner)}; !slices.Equal(owners, want) {
		t.Fatalf("owners = %v, want %v", owners, want)
	}
	if eventType != string(event.TypeTransferCompleted) || transferID != string(saved.ID()) {
		t.Fatalf("event = %q for %q", eventType, transferID)
	}
}

func TestPosting_BuildJournalUnitOfWork_WorkFails_RollsEverythingBack(t *testing.T) {
	fixture := newTransferFixture(t)
	created := fixture.newTransfer(t, 100, testOwner, "key-1")
	journal, err := created.BuildJournal([2]string{testID(101), testID(102)})
	if err != nil {
		t.Fatalf("Journal() error = %v", err)
	}
	err = postgres.NewTransferUnitOfWork(fixture.pool).Run(context.Background(), func(ctx context.Context, store transferservice.Store) error {
		if err := store.Ledger.Append(ctx, journal); err != nil {
			return err
		}
		if err := store.Transfers.Save(ctx, created, testFingerprint); err != nil {
			return err
		}
		metadata := event.Metadata{ID: event.ID(testID(200)), At: testTime}
		if err := store.Outbox.Add(ctx, event.NewTransferEvent(metadata, created, []apikey.OwnerID{testOwner})); err != nil {
			return err
		}
		return errDomain
	})
	if !errors.Is(err, errDomain) {
		t.Fatalf("Run() error = %v, want %v", err, errDomain)
	}
	for _, table := range []string{"transfers", "ledger_entries", "outbox_events"} {
		if rows := countRows(t, fixture.pool, table); rows != 0 {
			t.Errorf("%s rows = %d, want 0", table, rows)
		}
	}
}

func newRealService(fixture transferFixture) *transferservice.Service {
	return transferservice.NewMoney(postgres.NewTransferUnitOfWork(fixture.pool), fixture.repository, fixedClock{}, &sequentialIDs{})
}

func TestPosting_BuildJournalService_RealDatabase_MovesMoneyAndWritesOutbox(t *testing.T) {
	fixture := newTransferFixture(t)
	service := newRealService(fixture)
	ctx := context.Background()
	if _, err := service.Deposit(ctx, transferservice.FundingInput{
		OwnerID: string(testOwner), IdempotencyKey: "deposit-1", AccountID: string(fixture.from), Amount: "5.00", Currency: "BRL",
	}); err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}
	if _, err := service.Transfer(ctx, transferservice.TransferInput{
		OwnerID: string(testOwner), IdempotencyKey: "transfer-1", FromAccountID: string(fixture.from), ToAccountID: string(fixture.to), Amount: "1.00", Currency: "BRL",
	}); err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	ledgerRepository := postgres.NewLedgerRepository(fixture.pool)
	assertBalance(t, ledgerRepository, fixture.from, 400)
	assertBalance(t, ledgerRepository, fixture.to, 100)
	assertBalance(t, ledgerRepository, settlementBRL, -500)
	if rows := countRows(t, fixture.pool, "outbox_events"); rows != 2 {
		t.Fatalf("outbox rows = %d, want 2", rows)
	}
}

func TestPosting_BuildJournalService_SameKeyTwice_Replays(t *testing.T) {
	fixture := newTransferFixture(t)
	service := newRealService(fixture)
	ctx := context.Background()
	input := transferservice.FundingInput{
		OwnerID: string(testOwner), IdempotencyKey: "deposit-1", AccountID: string(fixture.from), Amount: "5.00", Currency: "BRL",
	}
	first, err := service.Deposit(ctx, input)
	if err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}
	second, err := service.Deposit(ctx, input)
	if err != nil {
		t.Fatalf("Deposit() replay error = %v", err)
	}
	if !second.IsReplay || second.ID != first.ID {
		t.Fatalf("replay = %+v, want replay of %s", second, first.ID)
	}
	if rows := countRows(t, fixture.pool, "transfers"); rows != 1 {
		t.Fatalf("transfer rows = %d, want 1", rows)
	}
}

func TestPosting_BuildJournalRepository_ClosedPool_ReturnsErrors(t *testing.T) {
	fixture := newTransferFixture(t)
	ctx := context.Background()
	saved := fixture.newTransfer(t, 100, testOwner, "key-1")
	fixture.pool.Close()
	_, listErr := fixture.repository.ListByAccount(ctx, fixture.from, page.Cursor{}, 1)
	for name, err := range map[string]error{
		"Save":          fixture.repository.Save(ctx, saved, testFingerprint),
		"MarkReversed":  fixture.repository.MarkReversed(ctx, saved),
		"ListByAccount": listErr,
	} {
		if err == nil {
			t.Errorf("%s() error = nil, want an error", name)
		}
	}
}
