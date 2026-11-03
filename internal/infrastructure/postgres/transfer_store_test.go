//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	transferservice "github.com/UrielJaloto/paygo/internal/services/transfer"
)

var errWorkFailed = errors.New("work failed")

func runTransferContract(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Helper()
	runCases(t, newStores, []contractCase{
		{"append moves balances and stores entries", ledgerAppend},
		{"append on one account applies every entry", ledgerSameAccount},
		{"balance of a missing account is not found", ledgerMissing},
		{"statement pages newest first", ledgerStatement},
		{"save then find by key round trips", transferRoundTrip},
		{"find missing is not found", transferMissing},
		{"duplicate key conflicts per owner", transferDuplicateKey},
		{"mark reversed changes the status", transferMarkReversed},
		{"list pages newest first", transferPages},
		{"failed work rolls everything back", transferRollback},
		{"service moves money and replays the key", transferService},
		{"service reverses once", transferReversal},
		{"claimed event carries its stored transfer", transferOutboxClaim},
	})
}

type pair struct {
	from account.ID
	to   account.ID
}

func newPair(t *testing.T, stores Stores) pair {
	t.Helper()
	return pair{from: storeCustomer(t, stores, 1).ID(), to: storeCustomer(t, stores, 2).ID()}
}

func (p pair) transfer(t *testing.T, sequence int, owner apikey.OwnerID) transfer.Transfer {
	t.Helper()
	created, err := transfer.New(transfer.ID(testID(sequence)), transfer.Request{
		OwnerID: owner,
		Key:     transfer.IdempotencyKey(testID(sequence)),
		Kind:    transfer.KindTransfer,
		From:    p.from,
		To:      p.to,
		Amount:  money.New(100, money.BRL),
	}, testTime)
	noError(t, err)
	return created
}

func (p pair) journal(t *testing.T, base int) ledger.JournalTransaction {
	t.Helper()
	journal, err := p.transfer(t, base, testOwner).Journal([2]string{testID(base + 1), testID(base + 2)})
	noError(t, err)
	return journal
}

func balanceOf(t *testing.T, stores Stores, id account.ID) int64 {
	t.Helper()
	balance, err := stores.Ledger.Balance(context.Background(), id)
	noError(t, err)
	equal(t, balance.Currency(), money.BRL)
	return balance.Minor()
}

func ledgerAppend(t *testing.T, stores Stores) {
	accounts := newPair(t, stores)
	noError(t, stores.Ledger.Append(context.Background(), accounts.journal(t, 100)))
	equal(t, balanceOf(t, stores, accounts.from), -100)
	equal(t, balanceOf(t, stores, accounts.to), 100)
	entries, err := stores.Ledger.Statement(context.Background(), accounts.to, page.Cursor{}, 10)
	noError(t, err)
	equal(t, len(entries), 1)
	equal(t, entries[0].JournalID(), testID(100))
	equal(t, entries[0].Direction(), ledger.Credit)
}

func ledgerSameAccount(t *testing.T, stores Stores) {
	id := storeCustomer(t, stores, 1).ID()
	debit, err := ledger.NewEntry(testID(101), id, ledger.Debit, money.New(100, money.BRL), testTime)
	noError(t, err)
	credit, err := ledger.NewEntry(testID(102), id, ledger.Credit, money.New(100, money.BRL), testTime)
	noError(t, err)
	journal, err := ledger.NewJournalTransaction(testID(100), []ledger.Entry{debit, credit})
	noError(t, err)
	noError(t, stores.Ledger.Append(context.Background(), journal))
	equal(t, balanceOf(t, stores, id), 0)
	entries, err := stores.Ledger.Statement(context.Background(), id, page.Cursor{}, 10)
	noError(t, err)
	equal(t, len(entries), 2)
}

func ledgerMissing(t *testing.T, stores Stores) {
	_, err := stores.Ledger.Balance(context.Background(), account.ID(testID(99)))
	errorIs(t, err, account.ErrAccountNotFound)
}

func ledgerStatement(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	noError(t, stores.Ledger.Append(ctx, accounts.journal(t, 100)))
	noError(t, stores.Ledger.Append(ctx, accounts.journal(t, 110)))
	newest, err := stores.Ledger.Statement(ctx, accounts.to, page.Cursor{}, 1)
	noError(t, err)
	equal(t, len(newest), 1)
	equal(t, newest[0].ID(), testID(112))
	older, err := stores.Ledger.Statement(ctx, accounts.to, page.NewCursor(testID(112)), 10)
	noError(t, err)
	equal(t, len(older), 1)
	equal(t, older[0].ID(), testID(102))
}

func transferRoundTrip(t *testing.T, stores Stores) {
	ctx := context.Background()
	created := newPair(t, stores).transfer(t, 100, testOwner)
	noError(t, stores.Transfers.Save(ctx, created, testFingerprint))
	found, stored, err := stores.Transfers.FindByIdempotencyKey(ctx, testOwner, created.IdempotencyKey())
	noError(t, err)
	equal(t, stored, testFingerprint)
	equal(t, found.ID(), created.ID())
	equal(t, found.Amount(), created.Amount())
	equal(t, found.Status(), transfer.StatusCompleted)
	equal(t, found.CreatedAt().Equal(testTime), true)
}

func transferMissing(t *testing.T, stores Stores) {
	ctx := context.Background()
	_, err := stores.Transfers.FindByID(ctx, transfer.ID(testID(99)))
	errorIs(t, err, transfer.ErrTransferNotFound)
	_, _, err = stores.Transfers.FindByIdempotencyKey(ctx, testOwner, "missing")
	errorIs(t, err, transfer.ErrTransferNotFound)
	_, err = stores.Transfers.FindByIDForUpdate(ctx, transfer.ID(testID(99)))
	errorIs(t, err, transfer.ErrTransferNotFound)
}

func transferDuplicateKey(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	noError(t, stores.Transfers.Save(ctx, accounts.transfer(t, 100, testOwner), testFingerprint))
	duplicate := accounts.transfer(t, 100, testOwner)
	repeated, err := transfer.New(transfer.ID(testID(101)), transfer.Request{
		OwnerID: testOwner, Key: duplicate.IdempotencyKey(), Kind: transfer.KindTransfer,
		From: accounts.from, To: accounts.to, Amount: duplicate.Amount(),
	}, testTime)
	noError(t, err)
	errorIs(t, stores.Transfers.Save(ctx, repeated, testFingerprint), transfer.ErrIdempotencyConflict)
	foreign, err := transfer.New(transfer.ID(testID(102)), transfer.Request{
		OwnerID: otherOwner, Key: duplicate.IdempotencyKey(), Kind: transfer.KindTransfer,
		From: accounts.from, To: accounts.to, Amount: duplicate.Amount(),
	}, testTime)
	noError(t, err)
	noError(t, stores.Transfers.Save(ctx, foreign, testFingerprint))
}

func transferMarkReversed(t *testing.T, stores Stores) {
	ctx := context.Background()
	created := newPair(t, stores).transfer(t, 100, testOwner)
	noError(t, stores.Transfers.Save(ctx, created, testFingerprint))
	noError(t, created.Reverse())
	noError(t, stores.Transfers.MarkReversed(ctx, created))
	found, err := stores.Transfers.FindByIDForUpdate(ctx, created.ID())
	noError(t, err)
	equal(t, found.Status(), transfer.StatusReversed)
}

func transferPages(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	older := accounts.transfer(t, 100, testOwner)
	newer := accounts.transfer(t, 101, testOwner)
	noError(t, stores.Transfers.Save(ctx, older, testFingerprint))
	noError(t, stores.Transfers.Save(ctx, newer, testFingerprint))
	opening, err := stores.Transfers.ListByAccount(ctx, accounts.to, page.Cursor{}, 1)
	noError(t, err)
	equal(t, len(opening), 1)
	equal(t, opening[0].ID(), newer.ID())
	rest, err := stores.Transfers.ListByAccount(ctx, accounts.from, page.NewCursor(string(newer.ID())), 10)
	noError(t, err)
	equal(t, len(rest), 1)
	equal(t, rest[0].ID(), older.ID())
}

func transferRollback(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	created := accounts.transfer(t, 100, testOwner)
	journal := accounts.journal(t, 100)
	err := stores.Unit.Run(ctx, func(ctx context.Context, store transferservice.Store) error {
		noError(t, store.Ledger.Append(ctx, journal))
		noError(t, store.Transfers.Save(ctx, created, testFingerprint))
		pending, err := store.Ledger.Balance(ctx, accounts.to)
		noError(t, err)
		equal(t, pending.Minor(), 100)
		return errWorkFailed
	})
	errorIs(t, err, errWorkFailed)
	equal(t, balanceOf(t, stores, accounts.to), 0)
	entries, err := stores.Ledger.Statement(ctx, accounts.to, page.Cursor{}, 10)
	noError(t, err)
	equal(t, len(entries), 0)
	_, _, err = stores.Transfers.FindByIdempotencyKey(ctx, testOwner, created.IdempotencyKey())
	errorIs(t, err, transfer.ErrTransferNotFound)
}

func transferOutboxClaim(t *testing.T, stores Stores) {
	ctx := context.Background()
	saved := newPair(t, stores).transfer(t, 10, testOwner)
	noError(t, stores.Transfers.Save(ctx, saved, testFingerprint))
	noError(t, stores.Outbox.Add(ctx, event.ForTransfer(event.Meta{ID: event.ID(testID(20)), At: testTime}, saved, []apikey.OwnerID{testOwner})))
	claimed, err := stores.Outbox.ClaimUnpublished(ctx, 1)
	noError(t, err)
	equal(t, len(claimed), 1)
	equal(t, claimed[0].Transfer().ID(), saved.ID())
	equal(t, claimed[0].Transfer().Amount(), saved.Amount())
}

func fund(t *testing.T, service *transferservice.Service, id account.ID) {
	t.Helper()
	_, err := service.Deposit(context.Background(), transferservice.FundingInput{
		OwnerID: string(testOwner), IdempotencyKey: "fund-" + string(id), AccountID: string(id), Amount: "10.00", Currency: "BRL",
	})
	noError(t, err)
}

func transferService(t *testing.T, stores Stores) {
	accounts := newPair(t, stores)
	service := transferservice.New(stores.Unit, stores.Transfers, fixedClock{}, &sequentialIDs{})
	fund(t, service, accounts.from)
	input := transferservice.TransferInput{
		OwnerID: string(testOwner), IdempotencyKey: "pay", FromAccountID: string(accounts.from),
		ToAccountID: string(accounts.to), Amount: "4.00", Currency: "BRL",
	}
	first, err := service.Transfer(context.Background(), input)
	noError(t, err)
	equal(t, first.IsReplay, false)
	second, err := service.Transfer(context.Background(), input)
	noError(t, err)
	equal(t, second.IsReplay, true)
	equal(t, second.ID, first.ID)
	equal(t, balanceOf(t, stores, accounts.from), 600)
	equal(t, balanceOf(t, stores, accounts.to), 400)
	equal(t, balanceOf(t, stores, settlementBRL), -1000)
	input.Amount = "5.00"
	_, err = service.Transfer(context.Background(), input)
	errorIs(t, err, transfer.ErrIdempotencyConflict)
	input.IdempotencyKey, input.Amount = "too-much", "100.00"
	_, err = service.Transfer(context.Background(), input)
	errorIs(t, err, transfer.ErrInsufficientFunds)
}

func transferReversal(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	service := transferservice.New(stores.Unit, stores.Transfers, fixedClock{}, &sequentialIDs{})
	fund(t, service, accounts.from)
	moved, err := service.Transfer(ctx, transferservice.TransferInput{
		OwnerID: string(testOwner), IdempotencyKey: "pay", FromAccountID: string(accounts.from),
		ToAccountID: string(accounts.to), Amount: "4.00", Currency: "BRL",
	})
	noError(t, err)
	_, err = service.Reverse(ctx, transferservice.ReverseInput{OwnerID: string(testOwner), TransferID: moved.ID, IdempotencyKey: "undo"})
	noError(t, err)
	equal(t, balanceOf(t, stores, accounts.from), 1000)
	equal(t, balanceOf(t, stores, accounts.to), 0)
	_, err = service.Reverse(ctx, transferservice.ReverseInput{OwnerID: string(testOwner), TransferID: moved.ID, IdempotencyKey: "undo-again"})
	errorIs(t, err, transfer.ErrTransferAlreadyReversed)
}
