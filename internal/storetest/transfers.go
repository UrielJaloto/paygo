package storetest

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
	transferservice "github.com/UrielJaloto/paygo/services/transfer"
)

const fingerprint transfer.Fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var errWorkFailed = errors.New("work failed")

type FixedClock struct{}

func (FixedClock) Now() time.Time { return At }

type SequentialIDs struct{ next atomic.Int64 }

func (s *SequentialIDs) NewID() string { return ID(int(s.next.Add(1)) + 10000) }

func RunTransferContract(t *testing.T, newStores func(t *testing.T) Stores) {
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
	})
}

type pair struct {
	from account.ID
	to   account.ID
}

func newPair(t *testing.T, stores Stores) pair {
	t.Helper()
	return pair{from: SaveCustomer(t, stores, 1).ID(), to: SaveCustomer(t, stores, 2).ID()}
}

func (p pair) transfer(t *testing.T, sequence int, owner apikey.OwnerID) transfer.Transfer {
	t.Helper()
	created, err := transfer.NewMoney(transfer.ID(ID(sequence)), transfer.Request{
		OwnerID: owner,
		Key:     transfer.IdempotencyKey(ID(sequence)),
		Kind:    transfer.KindTransfer,
		From:    p.from,
		To:      p.to,
		Amount:  money.NewMoney(100, money.BRL),
	}, At)
	NoError(t, err)
	return created
}

func (p pair) journal(t *testing.T, base int) ledger.JournalTransaction {
	t.Helper()
	journal, err := p.transfer(t, base, OwnerA).BuildJournal([2]string{ID(base + 1), ID(base + 2)})
	NoError(t, err)
	return journal
}

func balanceOf(t *testing.T, stores Stores, id account.ID) int64 {
	t.Helper()
	balance, err := stores.Ledger.CalculateBalance(context.Background(), id)
	NoError(t, err)
	Equal(t, balance.Currency(), money.BRL)
	return balance.Minor()
}

func ledgerAppend(t *testing.T, stores Stores) {
	accounts := newPair(t, stores)
	NoError(t, stores.Ledger.Append(context.Background(), accounts.journal(t, 100)))
	Equal(t, balanceOf(t, stores, accounts.from), -100)
	Equal(t, balanceOf(t, stores, accounts.to), 100)
	entries, err := stores.Ledger.ListStatement(context.Background(), accounts.to, page.Cursor{}, 10)
	NoError(t, err)
	Equal(t, len(entries), 1)
	Equal(t, entries[0].JournalID(), ID(100))
	Equal(t, entries[0].Direction(), ledger.Credit)
}

func ledgerSameAccount(t *testing.T, stores Stores) {
	id := SaveCustomer(t, stores, 1).ID()
	debit, err := ledger.NewEntry(ID(101), id, ledger.Debit, money.NewMoney(100, money.BRL), At)
	NoError(t, err)
	credit, err := ledger.NewEntry(ID(102), id, ledger.Credit, money.NewMoney(100, money.BRL), At)
	NoError(t, err)
	journal, err := ledger.NewJournalTransaction(ID(100), []ledger.Entry{debit, credit})
	NoError(t, err)
	NoError(t, stores.Ledger.Append(context.Background(), journal))
	Equal(t, balanceOf(t, stores, id), 0)
	entries, err := stores.Ledger.ListStatement(context.Background(), id, page.Cursor{}, 10)
	NoError(t, err)
	Equal(t, len(entries), 2)
}

func ledgerMissing(t *testing.T, stores Stores) {
	_, err := stores.Ledger.CalculateBalance(context.Background(), account.ID(ID(99)))
	ErrorIs(t, err, account.ErrAccountNotFound)
}

func ledgerStatement(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	NoError(t, stores.Ledger.Append(ctx, accounts.journal(t, 100)))
	NoError(t, stores.Ledger.Append(ctx, accounts.journal(t, 110)))
	newest, err := stores.Ledger.ListStatement(ctx, accounts.to, page.Cursor{}, 1)
	NoError(t, err)
	Equal(t, len(newest), 1)
	Equal(t, newest[0].ID(), ID(112))
	older, err := stores.Ledger.ListStatement(ctx, accounts.to, page.NewCursor(ID(112)), 10)
	NoError(t, err)
	Equal(t, len(older), 1)
	Equal(t, older[0].ID(), ID(102))
}

func transferRoundTrip(t *testing.T, stores Stores) {
	ctx := context.Background()
	created := newPair(t, stores).transfer(t, 100, OwnerA)
	NoError(t, stores.Transfers.Save(ctx, created, fingerprint))
	found, stored, err := stores.Transfers.FindByIdempotencyKey(ctx, OwnerA, created.IdempotencyKey())
	NoError(t, err)
	Equal(t, stored, fingerprint)
	Equal(t, found.ID(), created.ID())
	Equal(t, found.Amount(), created.Amount())
	Equal(t, found.Status(), transfer.StatusCompleted)
	Equal(t, found.CreatedAt().Equal(At), true)
}

func transferMissing(t *testing.T, stores Stores) {
	ctx := context.Background()
	_, err := stores.Transfers.FindByID(ctx, transfer.ID(ID(99)))
	ErrorIs(t, err, transfer.ErrTransferNotFound)
	_, _, err = stores.Transfers.FindByIdempotencyKey(ctx, OwnerA, "missing")
	ErrorIs(t, err, transfer.ErrTransferNotFound)
	_, err = stores.Transfers.FindByIDForUpdate(ctx, transfer.ID(ID(99)))
	ErrorIs(t, err, transfer.ErrTransferNotFound)
}

func transferDuplicateKey(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	NoError(t, stores.Transfers.Save(ctx, accounts.transfer(t, 100, OwnerA), fingerprint))
	duplicate := accounts.transfer(t, 100, OwnerA)
	repeated, err := transfer.NewMoney(transfer.ID(ID(101)), transfer.Request{
		OwnerID: OwnerA, Key: duplicate.IdempotencyKey(), Kind: transfer.KindTransfer,
		From: accounts.from, To: accounts.to, Amount: duplicate.Amount(),
	}, At)
	NoError(t, err)
	ErrorIs(t, stores.Transfers.Save(ctx, repeated, fingerprint), transfer.ErrIdempotencyConflict)
	otherOwner, err := transfer.NewMoney(transfer.ID(ID(102)), transfer.Request{
		OwnerID: OwnerB, Key: duplicate.IdempotencyKey(), Kind: transfer.KindTransfer,
		From: accounts.from, To: accounts.to, Amount: duplicate.Amount(),
	}, At)
	NoError(t, err)
	NoError(t, stores.Transfers.Save(ctx, otherOwner, fingerprint))
}

func transferMarkReversed(t *testing.T, stores Stores) {
	ctx := context.Background()
	created := newPair(t, stores).transfer(t, 100, OwnerA)
	NoError(t, stores.Transfers.Save(ctx, created, fingerprint))
	NoError(t, created.Reverse())
	NoError(t, stores.Transfers.MarkReversed(ctx, created))
	found, err := stores.Transfers.FindByIDForUpdate(ctx, created.ID())
	NoError(t, err)
	Equal(t, found.Status(), transfer.StatusReversed)
}

func transferPages(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	older := accounts.transfer(t, 100, OwnerA)
	newer := accounts.transfer(t, 101, OwnerA)
	NoError(t, stores.Transfers.Save(ctx, older, fingerprint))
	NoError(t, stores.Transfers.Save(ctx, newer, fingerprint))
	opening, err := stores.Transfers.ListByAccount(ctx, accounts.to, page.Cursor{}, 1)
	NoError(t, err)
	Equal(t, len(opening), 1)
	Equal(t, opening[0].ID(), newer.ID())
	rest, err := stores.Transfers.ListByAccount(ctx, accounts.from, page.NewCursor(string(newer.ID())), 10)
	NoError(t, err)
	Equal(t, len(rest), 1)
	Equal(t, rest[0].ID(), older.ID())
}

func transferRollback(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	created := accounts.transfer(t, 100, OwnerA)
	journal := accounts.journal(t, 100)
	err := stores.Unit.Run(ctx, func(ctx context.Context, store transferservice.Store) error {
		NoError(t, store.Ledger.Append(ctx, journal))
		NoError(t, store.Transfers.Save(ctx, created, fingerprint))
		pending, err := store.Ledger.CalculateBalance(ctx, accounts.to)
		NoError(t, err)
		Equal(t, pending.Minor(), 100)
		return errWorkFailed
	})
	ErrorIs(t, err, errWorkFailed)
	Equal(t, balanceOf(t, stores, accounts.to), 0)
	entries, err := stores.Ledger.ListStatement(ctx, accounts.to, page.Cursor{}, 10)
	NoError(t, err)
	Equal(t, len(entries), 0)
	_, _, err = stores.Transfers.FindByIdempotencyKey(ctx, OwnerA, created.IdempotencyKey())
	ErrorIs(t, err, transfer.ErrTransferNotFound)
}

func fund(t *testing.T, service *transferservice.Service, id account.ID) {
	t.Helper()
	_, err := service.Deposit(context.Background(), transferservice.FundingInput{
		OwnerID: string(OwnerA), IdempotencyKey: "fund-" + string(id), AccountID: string(id), Amount: "10.00", Currency: "BRL",
	})
	NoError(t, err)
}

func transferService(t *testing.T, stores Stores) {
	accounts := newPair(t, stores)
	service := transferservice.NewMoney(stores.Unit, stores.Transfers, FixedClock{}, &SequentialIDs{})
	fund(t, service, accounts.from)
	input := transferservice.TransferInput{
		OwnerID: string(OwnerA), IdempotencyKey: "pay", FromAccountID: string(accounts.from),
		ToAccountID: string(accounts.to), Amount: "4.00", Currency: "BRL",
	}
	first, err := service.Transfer(context.Background(), input)
	NoError(t, err)
	Equal(t, first.IsReplay, false)
	second, err := service.Transfer(context.Background(), input)
	NoError(t, err)
	Equal(t, second.IsReplay, true)
	Equal(t, second.ID, first.ID)
	Equal(t, balanceOf(t, stores, accounts.from), 600)
	Equal(t, balanceOf(t, stores, accounts.to), 400)
	Equal(t, balanceOf(t, stores, SettlementBRL), -1000)
	input.Amount = "5.00"
	_, err = service.Transfer(context.Background(), input)
	ErrorIs(t, err, transfer.ErrIdempotencyConflict)
	input.IdempotencyKey, input.Amount = "too-much", "100.00"
	_, err = service.Transfer(context.Background(), input)
	ErrorIs(t, err, transfer.ErrInsufficientFunds)
}

func transferReversal(t *testing.T, stores Stores) {
	ctx := context.Background()
	accounts := newPair(t, stores)
	service := transferservice.NewMoney(stores.Unit, stores.Transfers, FixedClock{}, &SequentialIDs{})
	fund(t, service, accounts.from)
	moved, err := service.Transfer(ctx, transferservice.TransferInput{
		OwnerID: string(OwnerA), IdempotencyKey: "pay", FromAccountID: string(accounts.from),
		ToAccountID: string(accounts.to), Amount: "4.00", Currency: "BRL",
	})
	NoError(t, err)
	_, err = service.Reverse(ctx, transferservice.ReverseInput{OwnerID: string(OwnerA), TransferID: moved.ID, IdempotencyKey: "undo"})
	NoError(t, err)
	Equal(t, balanceOf(t, stores, accounts.from), 1000)
	Equal(t, balanceOf(t, stores, accounts.to), 0)
	_, err = service.Reverse(ctx, transferservice.ReverseInput{OwnerID: string(OwnerA), TransferID: moved.ID, IdempotencyKey: "undo-again"})
	ErrorIs(t, err, transfer.ErrTransferAlreadyReversed)
}
