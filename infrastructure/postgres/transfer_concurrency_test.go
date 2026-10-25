//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	transferservice "github.com/UrielJaloto/paygo/services/transfer"
)

const (
	initialDeposit   = "100.00"
	initialMinor     = int64(10000)
	randomWorkers    = 50
	transfersByEach  = 20
	maximumRandomMinor   = 5000
	oppositeWorkers  = 200
	sameKeyWorkers   = 100
	customerAccounts = 10
)

type concurrencyFixture struct {
	pool     *pgxpool.Pool
	service  *transferservice.Service
	accounts []account.ID
}

func newConcurrencyFixture(t *testing.T, accounts int) concurrencyFixture {
	t.Helper()
	pool := newTestPool(t)
	repository := postgres.NewAccountRepository(pool)
	service := transferservice.NewMoney(postgres.NewTransferUnitOfWork(pool), postgres.NewTransferRepository(pool), fixedClock{}, &sequentialIDs{})
	fixture := concurrencyFixture{pool: pool, service: service}
	for sequence := range accounts {
		id := saveCustomer(t, repository, sequence, testOwner).ID()
		fixture.accounts = append(fixture.accounts, id)
		fixture.deposit(t, id, sequence)
	}
	return fixture
}

func (f concurrencyFixture) deposit(t *testing.T, id account.ID, sequence int) {
	t.Helper()
	_, err := f.service.Deposit(context.Background(), transferservice.FundingInput{
		OwnerID:        string(testOwner),
		IdempotencyKey: fmt.Sprintf("deposit-%d", sequence),
		AccountID:      string(id),
		Amount:         initialDeposit,
		Currency:       "BRL",
	})
	if err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}
}

func (f concurrencyFixture) send(key string, from, to account.ID, minor int64) error {
	_, err := f.service.Transfer(context.Background(), transferservice.TransferInput{
		OwnerID:        string(testOwner),
		IdempotencyKey: key,
		FromAccountID:  string(from),
		ToAccountID:    string(to),
		Amount:         fmt.Sprintf("%d.%02d", minor/100, minor%100),
		Currency:       "BRL",
	})
	return err
}

type outcomes struct {
	successes atomic.Int64
	failures  chan error
}

func newOutcomes(capacity int) *outcomes {
	return &outcomes{failures: make(chan error, capacity)}
}

func (o *outcomes) record(err error) {
	if err == nil {
		o.successes.Add(1)
		return
	}
	if !errors.Is(err, transfer.ErrInsufficientFunds) {
		o.failures <- err
	}
}

func (o *outcomes) assertOnlyAllowed(t *testing.T) {
	t.Helper()
	close(o.failures)
	for err := range o.failures {
		t.Errorf("unexpected transfer error: %v", err)
	}
}

func (f concurrencyFixture) assertInvariants(t *testing.T, successes int64) {
	t.Helper()
	ctx := context.Background()
	ledgerRepository := postgres.NewLedgerRepository(f.pool)
	var total int64
	for _, id := range f.accounts {
		balance, err := ledgerRepository.CalculateBalance(ctx, id)
		if err != nil {
			t.Fatalf("CalculateBalance() error = %v", err)
		}
		if balance.Minor() < 0 {
			t.Errorf("balance of %s = %d, want it not negative", id, balance.Minor())
		}
		total += balance.Minor()
	}
	assertBalance(t, ledgerRepository, settlementBRL, -total)
	if want := initialMinor * int64(len(f.accounts)); total != want {
		t.Errorf("customer total = %d, want %d", total, want)
	}
	mismatches, _, err := ledgerRepository.ListBalanceMismatches(ctx)
	if err != nil {
		t.Fatalf("CheckBalances() error = %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("mismatches = %+v, want none", mismatches)
	}
	var rows int64
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM transfers WHERE kind = 'transfer'").Scan(&rows); err != nil {
		t.Fatalf("counting transfers: %v", err)
	}
	if rows != successes {
		t.Errorf("transfer rows = %d, want %d", rows, successes)
	}
}

func (f concurrencyFixture) sendRandomly(worker int, results *outcomes) {
	random := rand.NewMoney(rand.NewPCG(uint64(worker), 0))
	for attempt := range transfersByEach {
		from := random.IntN(len(f.accounts))
		to := (from + 1 + random.IntN(len(f.accounts)-1)) % len(f.accounts)
		minor := 1 + random.Int64N(maximumRandomMinor)
		key := fmt.Sprintf("random-%d-%d", worker, attempt)
		results.record(f.send(key, f.accounts[from], f.accounts[to], minor))
	}
}

func TestPosting_BuildJournalConcurrency_RandomTransfers_KeepInvariants(t *testing.T) {
	fixture := newConcurrencyFixture(t, customerAccounts)
	results := newOutcomes(randomWorkers * transfersByEach)
	var workers sync.WaitGroup
	for worker := range randomWorkers {
		workers.Go(func() { fixture.sendRandomly(worker, results) })
	}
	workers.Wait()
	results.assertOnlyAllowed(t)
	fixture.assertInvariants(t, results.successes.Load())
}

func TestPosting_BuildJournalConcurrency_OppositeDirections_NoDeadlockSurfaces(t *testing.T) {
	fixture := newConcurrencyFixture(t, 2)
	results := newOutcomes(oppositeWorkers)
	var workers sync.WaitGroup
	for worker := range oppositeWorkers {
		from, to := fixture.accounts[worker%2], fixture.accounts[(worker+1)%2]
		workers.Go(func() { results.record(fixture.send(fmt.Sprintf("opposite-%d", worker), from, to, 100)) })
	}
	workers.Wait()
	results.assertOnlyAllowed(t)
	fixture.assertInvariants(t, results.successes.Load())
}

func TestPosting_BuildJournalConcurrency_SameKeyStorm_CreatesOneTransfer(t *testing.T) {
	fixture := newConcurrencyFixture(t, 2)
	ids := make([]string, sameKeyWorkers)
	failures := make([]error, sameKeyWorkers)
	var workers sync.WaitGroup
	for worker := range sameKeyWorkers {
		workers.Go(func() {
			output, err := fixture.service.Transfer(context.Background(), transferservice.TransferInput{
				OwnerID:        string(testOwner),
				IdempotencyKey: "storm",
				FromAccountID:  string(fixture.accounts[0]),
				ToAccountID:    string(fixture.accounts[1]),
				Amount:         "1.00",
				Currency:       "BRL",
			})
			ids[worker], failures[worker] = output.ID, err
		})
	}
	workers.Wait()
	if err := errors.Join(failures...); err != nil {
		t.Fatalf("Transfer() errors = %v", err)
	}
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("id = %s, want %s", id, ids[0])
		}
	}
	fixture.assertInvariants(t, 1)
}
