package transfer_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	service "github.com/UrielJaloto/paygo/internal/services/transfer"
)

const (
	ownerOne   = "0190a000-0000-7000-8000-000000000001"
	ownerTwo   = "0190a000-0000-7000-8000-000000000002"
	lowID      = "0190b000-0000-7000-8000-000000000001"
	highID     = "0190b000-0000-7000-8000-000000000002"
	settlement = "01900000-0000-7000-8000-000000000001"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func customer(id account.ID, owner apikey.OwnerID) account.Account {
	return account.Restore(account.Snapshot{ID: id, OwnerID: owner, Currency: money.BRL, Kind: account.KindCustomer, Status: account.StatusActive, Version: 1, CreatedAt: now})
}

func frozen(id account.ID, owner apikey.OwnerID) account.Account {
	return account.Restore(account.Snapshot{ID: id, OwnerID: owner, Currency: money.BRL, Kind: account.KindCustomer, Status: account.StatusFrozen, Version: 1, CreatedAt: now})
}

func settlementAccount() account.Account {
	return account.Restore(account.Snapshot{ID: settlement, OwnerID: apikey.SystemOwnerID, Currency: money.BRL, Kind: account.KindSettlement, Status: account.StatusActive, Version: 1, CreatedAt: now})
}

func standardStore() *fakeStore {
	store := newFakeStore(customer(lowID, ownerOne), customer(highID, ownerTwo), settlementAccount())
	store.balances[lowID] = money.NewMoney(500, money.BRL)
	store.balances[highID] = money.NewMoney(500, money.BRL)
	return store
}

func newService(store *fakeStore) (*service.Service, *fakeUnitOfWork) {
	unitOfWork := &fakeUnitOfWork{store: store}
	return service.NewService(unitOfWork, &fakeReader{}, stubClock{now: now}.Now, &sequenceIDs{}), unitOfWork
}

func validTransfer() service.TransferInput {
	return service.TransferInput{OwnerID: ownerOne, IdempotencyKey: "key-1", FromAccountID: lowID, ToAccountID: highID, Amount: "1.00", Currency: "BRL"}
}

func validFunding() service.FundingInput {
	return service.FundingInput{OwnerID: ownerTwo, IdempotencyKey: "key-1", AccountID: lowID, Amount: "1.00", Currency: "BRL"}
}

func TestTransfer_Valid_ReturnsCompletedOutput(t *testing.T) {
	transfers, _ := newService(standardStore())
	got, err := transfers.Transfer(context.Background(), validTransfer())
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	want := service.TransferOutput{ID: "id-1", Kind: "transfer", Status: "completed", FromAccountID: lowID, ToAccountID: highID, AmountMinor: 100, Currency: "BRL", CreatedAt: now}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestTransfer_Valid_AppendsBalancedJournal(t *testing.T) {
	store := standardStore()
	service, _ := newService(store)
	if _, err := service.Transfer(context.Background(), validTransfer()); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	entries := store.journals[0].Entries()
	if entries[0].AccountID() != lowID || entries[0].Direction() != ledger.Debit || entries[0].Amount().Minor() != 100 {
		t.Fatalf("debit = %+v", entries[0])
	}
	if entries[1].AccountID() != highID || entries[1].Direction() != ledger.Credit || entries[1].Amount().Minor() != 100 {
		t.Fatalf("credit = %+v", entries[1])
	}
}

func TestTransfer_Valid_SavesFingerprintAndNotifiesBothOwners(t *testing.T) {
	store := standardStore()
	service, _ := newService(store)
	if _, err := service.Transfer(context.Background(), validTransfer()); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	request := transfer.Request{Kind: transfer.KindTransfer, From: lowID, To: highID, Amount: money.NewMoney(100, money.BRL)}
	if store.saved[0].fingerprint != request.CalculateFingerprint() {
		t.Fatalf("fingerprint = %s", store.saved[0].fingerprint)
	}
	if !slices.Equal(store.events[0].OwnerIDs(), []apikey.OwnerID{ownerOne, ownerTwo}) {
		t.Fatalf("owners = %v", store.events[0].OwnerIDs())
	}
}

func TestTransfer_ExactBalance_LeavesZero(t *testing.T) {
	store := standardStore()
	store.balances[lowID] = money.NewMoney(100, money.BRL)
	service, _ := newService(store)
	if _, err := service.Transfer(context.Background(), validTransfer()); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
}

func TestTransfer_InsufficientFunds_WritesNothing(t *testing.T) {
	store := standardStore()
	store.balances[lowID] = money.NewMoney(99, money.BRL)
	service, _ := newService(store)
	_, err := service.Transfer(context.Background(), validTransfer())
	if !errors.Is(err, transfer.ErrInsufficientFunds) {
		t.Fatalf("error = %v", err)
	}
	if store.wasCalled("append") || store.wasCalled("save") || store.wasCalled("outbox") {
		t.Fatalf("calls = %v", store.calls)
	}
}

func TestTransfer_RejectedByAccounts_ReturnsExactError(t *testing.T) {
	cases := []struct {
		name  string
		store *fakeStore
		want  error
	}{
		{name: "source not owned", store: newFakeStore(customer(lowID, ownerTwo), customer(highID, ownerTwo)), want: account.ErrAccountNotFound},
		{name: "frozen source", store: newFakeStore(frozen(lowID, ownerOne), customer(highID, ownerTwo)), want: account.ErrAccountFrozen},
		{name: "frozen destination", store: newFakeStore(customer(lowID, ownerOne), frozen(highID, ownerTwo)), want: account.ErrAccountFrozen},
		{name: "missing account", store: newFakeStore(customer(lowID, ownerOne)), want: account.ErrAccountNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, _ := newService(testCase.store)
			_, err := service.Transfer(context.Background(), validTransfer())
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if testCase.store.wasCalled("append") {
				t.Fatalf("calls = %v", testCase.store.calls)
			}
		})
	}
}

func TestTransfer_CurrencyMismatch_ReturnsErrCurrencyMismatch(t *testing.T) {
	input := validTransfer()
	input.Currency = "USD"
	store := standardStore()
	service, _ := newService(store)
	_, err := service.Transfer(context.Background(), input)
	if !errors.Is(err, money.ErrCurrencyMismatch) || store.wasCalled("balance") {
		t.Fatalf("error = %v, calls = %v", err, store.calls)
	}
}

func TestTransfer_BalanceInOtherCurrency_ReturnsErrCurrencyMismatch(t *testing.T) {
	store := standardStore()
	store.balances[lowID] = money.NewMoney(500, money.USD)
	service, _ := newService(store)
	if _, err := service.Transfer(context.Background(), validTransfer()); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestTransfer_InvalidInput_DoesNotRunUnitOfWork(t *testing.T) {
	cases := []struct {
		name   string
		change func(*service.TransferInput)
		want   error
	}{
		{name: "same account", change: func(input *service.TransferInput) { input.ToAccountID = input.FromAccountID }, want: transfer.ErrSameAccount},
		{name: "invalid key", change: func(input *service.TransferInput) { input.IdempotencyKey = "" }, want: transfer.ErrInvalidIdempotencyKey},
		{name: "invalid amount", change: func(input *service.TransferInput) { input.Amount = "0" }, want: money.ErrInvalidAmount},
		{name: "invalid currency", change: func(input *service.TransferInput) { input.Currency = "GBP" }, want: money.ErrUnsupportedCurrency},
		{name: "invalid owner", change: func(input *service.TransferInput) { input.OwnerID = "x" }, want: apikey.ErrInvalidOwner},
		{name: "invalid source", change: func(input *service.TransferInput) { input.FromAccountID = "1" }, want: account.ErrInvalidAccountID},
		{name: "invalid destination", change: func(input *service.TransferInput) { input.ToAccountID = "1" }, want: account.ErrInvalidAccountID},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := validTransfer()
			testCase.change(&input)
			service, unitOfWork := newService(standardStore())
			if _, err := service.Transfer(context.Background(), input); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if unitOfWork.runs != 0 {
				t.Fatalf("runs = %d, want 0", unitOfWork.runs)
			}
		})
	}
}

func TestTransfer_HigherSourceID_LocksInAscendingOrder(t *testing.T) {
	store := newFakeStore(customer(lowID, ownerTwo), customer(highID, ownerOne))
	store.balances[highID] = money.NewMoney(500, money.BRL)
	input := validTransfer()
	input.FromAccountID, input.ToAccountID = highID, lowID
	service, _ := newService(store)
	got, err := service.Transfer(context.Background(), input)
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if !slices.Equal(store.locked, []account.ID{lowID, highID}) || got.FromAccountID != highID {
		t.Fatalf("locked = %v, output = %+v", store.locked, got)
	}
}

func TestTransfer_SameKeySameRequest_ReturnsReplay(t *testing.T) {
	request := transfer.Request{Kind: transfer.KindTransfer, From: lowID, To: highID, Amount: money.NewMoney(100, money.BRL)}
	original := transfer.Restore(transfer.Snapshot{ID: "t1", Request: request, Status: transfer.StatusCompleted, CreatedAt: now})
	store := standardStore()
	store.existing = &savedTransfer{transfer: original, fingerprint: request.CalculateFingerprint()}
	service, _ := newService(store)
	got, err := service.Transfer(context.Background(), validTransfer())
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if !got.IsReplay || got.ID != "t1" || !slices.Equal(store.calls, []string{"find"}) {
		t.Fatalf("got %+v, calls = %v", got, store.calls)
	}
}

func TestTransfer_SameKeyOtherRequest_ReturnsConflict(t *testing.T) {
	store := standardStore()
	store.existing = &savedTransfer{fingerprint: "other"}
	service, _ := newService(store)
	_, err := service.Transfer(context.Background(), validTransfer())
	if !errors.Is(err, transfer.ErrIdempotencyConflict) || store.wasCalled("lock") {
		t.Fatalf("error = %v, calls = %v", err, store.calls)
	}
}

func TestDeposit_Valid_MovesFromSettlement(t *testing.T) {
	store := standardStore()
	service, _ := newService(store)
	got, err := service.Deposit(context.Background(), validFunding())
	if err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if got.Kind != "deposit" || got.FromAccountID != settlement || got.ToAccountID != lowID {
		t.Fatalf("got %+v", got)
	}
	if store.wasCalled("balance") {
		t.Fatalf("settlement balance must not be checked: %v", store.calls)
	}
	if !slices.Equal(store.events[0].OwnerIDs(), []apikey.OwnerID{ownerOne}) {
		t.Fatalf("owners = %v", store.events[0].OwnerIDs())
	}
}

func TestDeposit_Rejected_ReturnsExactError(t *testing.T) {
	cases := []struct {
		name   string
		change func(*service.FundingInput)
		want   error
	}{
		{name: "settlement account", change: func(input *service.FundingInput) { input.AccountID = settlement }, want: account.ErrAccountNotFound},
		{name: "invalid owner", change: func(input *service.FundingInput) { input.OwnerID = "x" }, want: apikey.ErrInvalidOwner},
		{name: "invalid key", change: func(input *service.FundingInput) { input.IdempotencyKey = "" }, want: transfer.ErrInvalidIdempotencyKey},
		{name: "invalid account", change: func(input *service.FundingInput) { input.AccountID = "1" }, want: account.ErrInvalidAccountID},
		{name: "invalid amount", change: func(input *service.FundingInput) { input.Amount = "0" }, want: money.ErrInvalidAmount},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := validFunding()
			testCase.change(&input)
			service, unitOfWork := newService(standardStore())
			if _, err := service.Deposit(context.Background(), input); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if unitOfWork.runs != 0 {
				t.Fatalf("runs = %d, want 0", unitOfWork.runs)
			}
		})
	}
}

func TestDeposit_NonCustomerAccount_ReturnsErrAccountNotFound(t *testing.T) {
	clearing := account.Restore(account.Snapshot{ID: lowID, Currency: money.BRL, Kind: account.KindProviderClearing, Status: account.StatusActive})
	service, _ := newService(newFakeStore(clearing, settlementAccount()))
	if _, err := service.Deposit(context.Background(), validFunding()); !errors.Is(err, account.ErrAccountNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestWithdraw_Valid_MovesToSettlement(t *testing.T) {
	service, _ := newService(standardStore())
	got, err := service.Withdraw(context.Background(), validFunding())
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if got.Kind != "withdrawal" || got.FromAccountID != lowID || got.ToAccountID != settlement {
		t.Fatalf("got %+v", got)
	}
}

func TestWithdraw_InsufficientFunds_ReturnsErrInsufficientFunds(t *testing.T) {
	store := standardStore()
	store.balances[lowID] = money.NewMoney(99, money.BRL)
	service, _ := newService(store)
	if _, err := service.Withdraw(context.Background(), validFunding()); !errors.Is(err, transfer.ErrInsufficientFunds) {
		t.Fatalf("error = %v", err)
	}
}

func TestWithdraw_InvalidInput_DoesNotRunUnitOfWork(t *testing.T) {
	input := validFunding()
	input.Amount = "0"
	service, unitOfWork := newService(standardStore())
	if _, err := service.Withdraw(context.Background(), input); !errors.Is(err, money.ErrInvalidAmount) || unitOfWork.runs != 0 {
		t.Fatalf("error = %v, runs = %d", err, unitOfWork.runs)
	}
}

func TestTransfer_UnitOfWorkFails_WrapsError(t *testing.T) {
	store := standardStore()
	service, unitOfWork := newService(store)
	unitOfWork.err = errBoom
	if _, err := service.Transfer(context.Background(), validTransfer()); !errors.Is(err, errBoom) {
		t.Fatalf("error = %v", err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("calls = %v", store.calls)
	}
}

func TestTransfer_StoreCallFails_StopsLaterCalls(t *testing.T) {
	order := []string{"find", "lock", "find", "balance", "append", "save", "outbox"}
	for index, failing := range order {
		t.Run(fmt.Sprintf("%d_%s", index, failing), func(t *testing.T) {
			store := standardStore()
			store.failOnCall = index + 1
			service, _ := newService(store)
			if _, err := service.Transfer(context.Background(), validTransfer()); !errors.Is(err, errBoom) {
				t.Fatalf("error = %v", err)
			}
			if !slices.Equal(store.calls, order[:index+1]) {
				t.Fatalf("calls = %v, want %v", store.calls, order[:index+1])
			}
		})
	}
}

func TestTransfer_SameKeyCommittedWhileWaitingForLock_ReturnsReplay(t *testing.T) {
	request := transfer.Request{Kind: transfer.KindTransfer, From: lowID, To: highID, Amount: money.NewMoney(100, money.BRL)}
	original := transfer.Restore(transfer.Snapshot{ID: "t1", Request: request, Status: transfer.StatusCompleted, CreatedAt: now})
	store := standardStore()
	store.existingAfterLock = &savedTransfer{transfer: original, fingerprint: request.CalculateFingerprint()}
	service, _ := newService(store)
	got, err := service.Transfer(context.Background(), validTransfer())
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if !got.IsReplay || got.ID != "t1" || !slices.Equal(store.calls, []string{"find", "lock", "find"}) {
		t.Fatalf("got %+v, calls = %v", got, store.calls)
	}
}
