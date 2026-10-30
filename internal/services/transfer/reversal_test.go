package transfer_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	service "github.com/UrielJaloto/paygo/internal/services/transfer"
)

const originalID = "0190c000-0000-7000-8000-000000000001"

func completed(id transfer.ID, kind transfer.Kind, owner apikey.OwnerID) transfer.Transfer {
	request := transfer.Request{OwnerID: owner, Key: "key-0", Kind: kind, From: lowID, To: highID, Amount: money.NewMoney(100, money.BRL)}
	return transfer.Restore(transfer.Snapshot{ID: id, Request: request, Status: transfer.StatusCompleted, CreatedAt: now})
}

func reversibleStore() *fakeStore {
	store := standardStore()
	store.originals[originalID] = completed(originalID, transfer.KindTransfer, ownerOne)
	return store
}

func validReverse() service.ReverseInput {
	return service.ReverseInput{OwnerID: ownerOne, IdempotencyKey: "key-1", TransferID: originalID}
}

func TestReverse_Valid_MovesBackAndMarksOriginal(t *testing.T) {
	store := reversibleStore()
	svc, _ := newService(store)
	got, err := svc.Reverse(context.Background(), validReverse())
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if got.Kind != "reversal" || got.FromAccountID != highID || got.ToAccountID != lowID || got.ReversalOf != originalID {
		t.Fatalf("got %+v", got)
	}
	if store.marked[0].Status() != transfer.StatusReversed {
		t.Fatalf("status = %s", store.marked[0].Status())
	}
	if store.events[0].Type() != event.TypeTransferReversed {
		t.Fatalf("event type = %s", store.events[0].Type())
	}
}

func TestReverse_InsufficientFunds_DoesNotMarkOriginal(t *testing.T) {
	store := reversibleStore()
	store.balances[highID] = money.NewMoney(50, money.BRL)
	svc, _ := newService(store)
	_, err := svc.Reverse(context.Background(), validReverse())
	if !errors.Is(err, transfer.ErrInsufficientFunds) || store.wasCalled("mark") {
		t.Fatalf("error = %v, calls = %v", err, store.calls)
	}
}

func TestReverse_NotReversible_ReturnsExactError(t *testing.T) {
	alreadyReversed := completed(originalID, transfer.KindTransfer, ownerOne)
	if err := alreadyReversed.Reverse(); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	cases := []struct {
		name     string
		original transfer.Transfer
		want     error
	}{
		{name: "already reversed", original: alreadyReversed, want: transfer.ErrTransferAlreadyReversed},
		{name: "reversal kind", original: completed(originalID, transfer.KindReversal, ownerOne), want: transfer.ErrReversalNotReversible},
		{name: "other owner", original: completed(originalID, transfer.KindTransfer, ownerTwo), want: transfer.ErrTransferNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := standardStore()
			store.originals[originalID] = testCase.original
			svc, _ := newService(store)
			_, err := svc.Reverse(context.Background(), validReverse())
			if !errors.Is(err, testCase.want) || store.wasCalled("lock") {
				t.Fatalf("error = %v, calls = %v", err, store.calls)
			}
		})
	}
}

func TestReverse_SameKey_ReturnsReplayAfterOriginalWasReversed(t *testing.T) {
	store := reversibleStore()
	original := store.originals[originalID]
	request, err := original.BuildReversalRequest("key-1")
	if err != nil {
		t.Fatalf("ReversalRequest: %v", err)
	}
	if err := original.Reverse(); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	store.originals[originalID] = original
	done := transfer.Restore(transfer.Snapshot{ID: "r1", Request: request, Status: transfer.StatusCompleted, CreatedAt: now})
	store.existing = &savedTransfer{transfer: done, fingerprint: request.CalculateFingerprint()}
	svc, _ := newService(store)
	got, err := svc.Reverse(context.Background(), validReverse())
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if !got.IsReplay || got.ID != "r1" || store.wasCalled("lock") {
		t.Fatalf("got %+v, calls = %v", got, store.calls)
	}
}

func TestReverse_Deposit_MovesBackToSettlement(t *testing.T) {
	request := transfer.Request{OwnerID: apikey.SystemOwnerID, Key: "key-0", Kind: transfer.KindDeposit, From: settlement, To: lowID, Amount: money.NewMoney(100, money.BRL)}
	store := standardStore()
	store.originals[originalID] = transfer.Restore(transfer.Snapshot{ID: originalID, Request: request, Status: transfer.StatusCompleted, CreatedAt: now})
	svc, _ := newService(store)
	input := validReverse()
	input.OwnerID = string(apikey.SystemOwnerID)
	got, err := svc.Reverse(context.Background(), input)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if got.FromAccountID != lowID || got.ToAccountID != settlement {
		t.Fatalf("got %+v", got)
	}
}

func TestReverse_InvalidInput_DoesNotRunUnitOfWork(t *testing.T) {
	cases := []struct {
		name   string
		change func(*service.ReverseInput)
		want   error
	}{
		{name: "invalid owner", change: func(i *service.ReverseInput) { i.OwnerID = "x" }, want: apikey.ErrInvalidOwner},
		{name: "invalid key", change: func(i *service.ReverseInput) { i.IdempotencyKey = "" }, want: transfer.ErrInvalidIdempotencyKey},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := validReverse()
			testCase.change(&input)
			svc, unitOfWork := newService(reversibleStore())
			if _, err := svc.Reverse(context.Background(), input); !errors.Is(err, testCase.want) || unitOfWork.runs != 0 {
				t.Fatalf("error = %v, runs = %d", err, unitOfWork.runs)
			}
		})
	}
}

func TestReverse_StoreCallFails_StopsLaterCalls(t *testing.T) {
	order := []string{"original", "find", "lock", "find", "balance", "append", "save", "outbox", "mark"}
	for i, failing := range order {
		t.Run(fmt.Sprintf("%d_%s", i, failing), func(t *testing.T) {
			store := reversibleStore()
			store.failOnCall = i + 1
			svc, _ := newService(store)
			if _, err := svc.Reverse(context.Background(), validReverse()); !errors.Is(err, errBoom) {
				t.Fatalf("error = %v", err)
			}
			if !slices.Equal(store.calls, order[:i+1]) {
				t.Fatalf("calls = %v, want %v", store.calls, order[:i+1])
			}
		})
	}
}

func newQueryService(reader *fakeReader) *service.Service {
	return service.NewMoney(&fakeUnitOfWork{store: standardStore()}, reader, stubClock{now: now}, &sequenceIDs{})
}

func TestFind_Outcomes_ReturnExactResult(t *testing.T) {
	found := map[transfer.ID]transfer.Transfer{
		"mine":  completed("mine", transfer.KindTransfer, ownerOne),
		"other": completed("other", transfer.KindTransfer, ownerTwo),
	}
	cases := []struct {
		name string
		id   string
		want error
	}{
		{name: "own", id: "mine"},
		{name: "other owner", id: "other", want: transfer.ErrTransferNotFound},
		{name: "missing", id: "none", want: transfer.ErrTransferNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc := newQueryService(&fakeReader{found: found})
			got, err := svc.Get(context.Background(), service.TransferQuery{OwnerID: ownerOne, TransferID: testCase.id})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if testCase.want == nil && got.ID != testCase.id {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestFind_Failures_ReturnExactError(t *testing.T) {
	svc := newQueryService(&fakeReader{findErr: errBoom})
	if _, err := svc.Get(context.Background(), service.TransferQuery{OwnerID: ownerOne, TransferID: "t"}); !errors.Is(err, errBoom) {
		t.Fatalf("error = %v", err)
	}
	if _, err := svc.Get(context.Background(), service.TransferQuery{OwnerID: "x"}); !errors.Is(err, apikey.ErrInvalidOwner) {
		t.Fatalf("error = %v", err)
	}
}

func TestList_MoreThanLimit_ReturnsPageAndCursor(t *testing.T) {
	reader := &fakeReader{listed: []transfer.Transfer{
		completed("t1", transfer.KindTransfer, ownerOne),
		completed("t2", transfer.KindTransfer, ownerOne),
		completed("t3", transfer.KindTransfer, ownerOne),
	}}
	got, err := newQueryService(reader).List(context.Background(), service.ListInput{OwnerID: ownerOne, AccountID: lowID, Limit: "2"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got.Items) != 2 || got.NextCursor != page.NewCursor("t2").Encode() || reader.limit != 3 {
		t.Fatalf("got %+v, limit = %d", got, reader.limit)
	}
}

func TestList_ForeignAccount_RevealsNothing(t *testing.T) {
	reader := &fakeReader{listed: []transfer.Transfer{
		completed("t1", transfer.KindTransfer, ownerTwo),
		completed("t2", transfer.KindTransfer, ownerTwo),
	}}
	got, err := newQueryService(reader).List(context.Background(), service.ListInput{OwnerID: ownerOne, AccountID: lowID, Limit: "1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got.Items) != 0 || got.NextCursor != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestList_Failures_ReturnExactError(t *testing.T) {
	valid := service.ListInput{OwnerID: ownerOne, AccountID: lowID}
	cases := []struct {
		name   string
		change func(*service.ListInput)
		want   error
	}{
		{name: "invalid owner", change: func(i *service.ListInput) { i.OwnerID = "x" }, want: apikey.ErrInvalidOwner},
		{name: "invalid account", change: func(i *service.ListInput) { i.AccountID = "1" }, want: account.ErrInvalidAccountID},
		{name: "invalid cursor", change: func(i *service.ListInput) { i.Cursor = "%" }, want: page.ErrInvalidCursor},
		{name: "invalid limit", change: func(i *service.ListInput) { i.Limit = "x" }, want: page.ErrInvalidPageLimit},
		{name: "reader fails", change: func(*service.ListInput) {}, want: errBoom},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := valid
			testCase.change(&input)
			svc := newQueryService(&fakeReader{listErr: errBoom})
			if _, err := svc.List(context.Background(), input); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestReverse_OriginalCannotBePosted_ReturnsDomainError(t *testing.T) {
	cases := []struct {
		name   string
		to     account.ID
		amount int64
		want   error
	}{
		{name: "same account", to: lowID, amount: 100, want: transfer.ErrSameAccount},
		{name: "zero amount", to: highID, amount: 0, want: money.ErrInvalidAmount},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := standardStore()
			store.balances[lowID] = money.NewMoney(1000, money.BRL)
			store.balances[highID] = money.NewMoney(1000, money.BRL)
			request := transfer.Request{OwnerID: ownerOne, Key: "key-0", Kind: transfer.KindTransfer, From: lowID, To: testCase.to, Amount: money.NewMoney(testCase.amount, money.BRL)}
			store.originals[originalID] = transfer.Restore(transfer.Snapshot{ID: originalID, Request: request, Status: transfer.StatusCompleted, CreatedAt: now})
			svc, _ := newService(store)
			if _, err := svc.Reverse(context.Background(), validReverse()); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
