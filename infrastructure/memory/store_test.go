package memory_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/infrastructure/memory"
	"github.com/UrielJaloto/paygo/internal/storetest"
	transferservice "github.com/UrielJaloto/paygo/services/transfer"
)

const (
	concurrentWorkers   = 50
	transfersPerWorker  = 20
	concurrentAccounts  = 10
	concurrentInitial   = int64(10000)
	concurrentMaxAmount = 5000
	initialAmount       = "100.00"
)

func newStores(store *memory.Store) storetest.Stores {
	return storetest.Stores{
		Accounts:  memory.NewAccountRepository(store),
		Ledger:    memory.NewLedgerRepository(store),
		Transfers: memory.NewTransferRepository(store),
		Unit:      memory.NewTransferUnitOfWork(store),
		Outbox:    memory.NewOutboxRepository(store),
	}
}

func newFreshStores(*testing.T) storetest.Stores {
	return newStores(memory.NewStore(storetest.FixedClock{}))
}

func TestAccountContract_MemoryStore_Passes(t *testing.T) {
	storetest.RunAccountContract(t, newFreshStores)
}

func TestPosting_BuildJournalContract_MemoryStore_Passes(t *testing.T) {
	storetest.RunTransferContract(t, newFreshStores)
}

func TestAccountSave_DuplicateID_ReturnsDuplicate(t *testing.T) {
	stores := newFreshStores(t)
	saved := storetest.SaveCustomer(t, stores, 1)
	storetest.ErrorIs(t, stores.Accounts.Save(context.Background(), saved), memory.ErrDuplicateID)
}

func TestAccountUpdate_Missing_ReturnsConcurrentModification(t *testing.T) {
	stores := newFreshStores(t)
	missing, err := account.NewCustomer(account.ID(storetest.ID(9)), storetest.OwnerA, money.BRL, storetest.At)
	storetest.NoError(t, err)
	storetest.ErrorIs(t, stores.Accounts.Update(context.Background(), missing, 1), account.ErrConcurrentModification)
}

func TestListings_UnknownAccount_AreEmpty(t *testing.T) {
	stores := newFreshStores(t)
	id := account.ID(storetest.ID(9))
	entries, err := stores.Ledger.ListStatement(context.Background(), id, page.Cursor{}, 10)
	storetest.NoError(t, err)
	storetest.Equal(t, len(entries), 0)
	moved, err := stores.Transfers.ListByAccount(context.Background(), id, page.Cursor{}, 10)
	storetest.NoError(t, err)
	storetest.Equal(t, len(moved), 0)
}

func newMovement(t *testing.T, stores storetest.Stores, key transfer.IdempotencyKey) transfer.Transfer {
	t.Helper()
	created, err := transfer.NewMoney(transfer.ID(storetest.ID(100)), transfer.Request{
		OwnerID: storetest.OwnerA,
		Key:     key,
		Kind:    transfer.KindTransfer,
		From:    storetest.SaveCustomer(t, stores, 1).ID(),
		To:      storetest.SaveCustomer(t, stores, 2).ID(),
		Amount:  money.NewMoney(100, money.BRL),
	}, storetest.At)
	storetest.NoError(t, err)
	return created
}

func TestLedgerAppend_UnknownAccount_ReturnsNotFound(t *testing.T) {
	stores := newFreshStores(t)
	journal, err := ledger.Posting{
		ID: storetest.ID(1), EntryIDs: [2]string{storetest.ID(2), storetest.ID(3)},
		From: account.ID(storetest.ID(8)), To: account.ID(storetest.ID(9)), Amount: money.NewMoney(100, money.BRL), At: storetest.At,
	}.BuildJournal()
	storetest.NoError(t, err)
	storetest.ErrorIs(t, stores.Ledger.Append(context.Background(), journal), account.ErrAccountNotFound)
}

func TestPosting_BuildJournalSave_UnknownAccount_ReturnsNotFound(t *testing.T) {
	stores := newFreshStores(t)
	created, err := transfer.NewMoney(transfer.ID(storetest.ID(1)), transfer.Request{
		OwnerID: storetest.OwnerA, Key: "key", Kind: transfer.KindTransfer,
		From: account.ID(storetest.ID(8)), To: account.ID(storetest.ID(9)), Amount: money.NewMoney(100, money.BRL),
	}, storetest.At)
	storetest.NoError(t, err)
	storetest.ErrorIs(t, stores.Transfers.Save(context.Background(), created, "print"), account.ErrAccountNotFound)
}

func TestPosting_BuildJournalMarkReversed_Unknown_ChangesNothing(t *testing.T) {
	stores := newFreshStores(t)
	created := newMovement(t, stores, "key")
	storetest.NoError(t, stores.Transfers.MarkReversed(context.Background(), created))
	_, err := stores.Transfers.FindByID(context.Background(), created.ID())
	storetest.ErrorIs(t, err, transfer.ErrTransferNotFound)
}

func TestPosting_BuildJournalUnitOfWork_KeyTakenBeforeCommit_ReturnsConflict(t *testing.T) {
	stores := newFreshStores(t)
	created := newMovement(t, stores, "key")
	err := stores.Unit.Run(context.Background(), func(ctx context.Context, store transferservice.Store) error {
		if err := store.Transfers.Save(ctx, created, "print"); err != nil {
			return err
		}
		return stores.Transfers.Save(ctx, created, "print")
	})
	storetest.ErrorIs(t, err, transfer.ErrIdempotencyConflict)
	found, _, err := stores.Transfers.FindByIdempotencyKey(context.Background(), storetest.OwnerA, "key")
	storetest.NoError(t, err)
	storetest.Equal(t, found.ID(), created.ID())
}

func TestPosting_BuildJournalUnitOfWork_LockTwice_DoesNotDeadlock(t *testing.T) {
	stores := newFreshStores(t)
	id := storetest.SaveCustomer(t, stores, 1).ID()
	err := stores.Unit.Run(context.Background(), func(ctx context.Context, store transferservice.Store) error {
		if _, err := store.Accounts.LockByIDs(ctx, []account.ID{id}); err != nil {
			return err
		}
		_, err := store.Accounts.LockByIDs(ctx, []account.ID{id})
		return err
	})
	storetest.NoError(t, err)
}

func TestOutboxAdd_OutsideUnit_Succeeds(t *testing.T) {
	store := memory.NewStore(storetest.FixedClock{})
	err := memory.NewOutboxRepository(store).Add(context.Background(), event.Restore(event.Snapshot{}))
	storetest.NoError(t, err)
}

type concurrentRun struct {
	service  *transferservice.Service
	stores   storetest.Stores
	accounts []account.ID
}

func newConcurrentRun(t testing.TB, accounts int) concurrentRun {
	t.Helper()
	stores := newStores(memory.NewStore(storetest.FixedClock{}))
	run := concurrentRun{
		service: transferservice.NewMoney(stores.Unit, stores.Transfers, storetest.FixedClock{}, &storetest.SequentialIDs{}),
		stores:  stores,
	}
	for sequence := range accounts {
		customer, err := account.NewCustomer(account.ID(storetest.ID(sequence)), storetest.OwnerA, money.BRL, storetest.At)
		storetest.NoError(t, err)
		storetest.NoError(t, stores.Accounts.Save(context.Background(), customer))
		run.accounts = append(run.accounts, customer.ID())
		_, err = run.service.Deposit(context.Background(), transferservice.FundingInput{
			OwnerID: string(storetest.OwnerA), IdempotencyKey: fmt.Sprintf("deposit-%d", sequence),
			AccountID: string(customer.ID()), Amount: initialAmount, Currency: "BRL",
		})
		storetest.NoError(t, err)
	}
	return run
}

func (r concurrentRun) send(key string, from, to account.ID, minor int64) error {
	_, err := r.service.Transfer(context.Background(), transferservice.TransferInput{
		OwnerID: string(storetest.OwnerA), IdempotencyKey: key, FromAccountID: string(from), ToAccountID: string(to),
		Amount: fmt.Sprintf("%d.%02d", minor/100, minor%100), Currency: "BRL",
	})
	return err
}

func (r concurrentRun) sendRandomly(worker int, failures chan<- error) {
	random := rand.New(rand.NewPCG(uint64(worker), 0))
	for attempt := range transfersPerWorker {
		from := random.IntN(len(r.accounts))
		to := (from + 1 + random.IntN(len(r.accounts)-1)) % len(r.accounts)
		err := r.send(fmt.Sprintf("random-%d-%d", worker, attempt), r.accounts[from], r.accounts[to], 1+random.Int64N(concurrentMaxAmount))
		if err != nil && !errors.Is(err, transfer.ErrInsufficientFunds) {
			failures <- err
		}
	}
}

func TestPosting_BuildJournalConcurrency_ThousandRandomTransfers_KeepInvariants(t *testing.T) {
	run := newConcurrentRun(t, concurrentAccounts)
	failures := make(chan error, concurrentWorkers*transfersPerWorker)
	var workers sync.WaitGroup
	for worker := range concurrentWorkers {
		workers.Go(func() { run.sendRandomly(worker, failures) })
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("unexpected transfer error: %v", err)
	}
	var total int64
	for _, id := range run.accounts {
		balance, err := run.stores.Ledger.CalculateBalance(context.Background(), id)
		storetest.NoError(t, err)
		storetest.Equal(t, balance.IsNegative(), false)
		total += balance.Minor()
	}
	storetest.Equal(t, total, concurrentInitial*concurrentAccounts)
	settlement, err := run.stores.Ledger.CalculateBalance(context.Background(), storetest.SettlementBRL)
	storetest.NoError(t, err)
	storetest.Equal(t, settlement.Minor(), -total)
}

func TestPosting_BuildJournalConcurrency_SameKeyStorm_MovesOnce(t *testing.T) {
	run := newConcurrentRun(t, 2)
	var conflicts atomic.Int64
	var workers sync.WaitGroup
	for range concurrentWorkers {
		workers.Go(func() {
			if err := run.send("storm", run.accounts[0], run.accounts[1], 100); err != nil {
				conflicts.Add(1)
			}
		})
	}
	workers.Wait()
	storetest.Equal(t, conflicts.Load(), 0)
	balance, err := run.stores.Ledger.CalculateBalance(context.Background(), run.accounts[1])
	storetest.NoError(t, err)
	storetest.Equal(t, balance.Minor(), concurrentInitial+100)
}

func BenchmarkMemoryTransfer(b *testing.B) {
	run := newConcurrentRun(b, 2)
	b.ReportAllocs()
	sequence := 0
	for b.Loop() {
		sequence++
		from, to := run.accounts[sequence%2], run.accounts[(sequence+1)%2]
		if err := run.send(fmt.Sprintf("bench-%d", sequence), from, to, 1); err != nil {
			b.Fatal(err)
		}
	}
}
