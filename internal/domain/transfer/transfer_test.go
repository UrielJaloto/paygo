package transfer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const (
	accountA account.ID     = "A"
	accountB account.ID     = "B"
	owner    apikey.OwnerID = "owner"
)

var createdAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func request() transfer.Request {
	return transfer.Request{
		OwnerID: owner,
		Key:     "key-1",
		Kind:    transfer.KindTransfer,
		From:    accountA,
		To:      accountB,
		Amount:  money.New(100, money.BRL),
	}
}

func newTransfer(t *testing.T) transfer.Transfer {
	t.Helper()
	created, err := transfer.New("t1", request(), createdAt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return created
}

func TestNew_Valid_IsCompleted(t *testing.T) {
	created := newTransfer(t)
	if created.ID() != "t1" || created.OwnerID() != owner || created.IdempotencyKey() != "key-1" {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Kind() != transfer.KindTransfer || created.Status() != transfer.StatusCompleted {
		t.Fatalf("unexpected state: %+v", created)
	}
}

func TestNew_Valid_KeepsMovement(t *testing.T) {
	created := newTransfer(t)
	if created.From() != accountA || created.To() != accountB || created.Amount() != money.New(100, money.BRL) {
		t.Fatalf("unexpected movement: %+v", created)
	}
	if created.ReversalOf() != "" || !created.CreatedAt().Equal(createdAt) {
		t.Fatalf("unexpected origin: %+v", created)
	}
}

func TestNew_SameAccount_ReturnsErrSameAccount(t *testing.T) {
	same := request()
	same.To = same.From
	if _, err := transfer.New("t1", same, createdAt); !errors.Is(err, transfer.ErrSameAccount) {
		t.Fatalf("error = %v, want ErrSameAccount", err)
	}
}

func TestParseIdempotencyKey_ValidatesFormat(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		wantErr error
	}{
		{name: "1 char", text: "a"},
		{name: "64 chars", text: strings.Repeat("a", 64)},
		{name: "65 chars", text: strings.Repeat("a", 65), wantErr: transfer.ErrInvalidIdempotencyKey},
		{name: "empty", text: "", wantErr: transfer.ErrInvalidIdempotencyKey},
		{name: "space", text: "a b", wantErr: transfer.ErrInvalidIdempotencyKey},
		{name: "accent", text: "é", wantErr: transfer.ErrInvalidIdempotencyKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := transfer.ParseIdempotencyKey(tc.text); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestFingerprintOf_IsExactAndStable(t *testing.T) {
	sum := sha256.Sum256([]byte("transfer|A|B|100|BRL"))
	want := transfer.Fingerprint(hex.EncodeToString(sum[:]))
	if got := transfer.FingerprintOf(request()); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if transfer.FingerprintOf(request()) != transfer.FingerprintOf(request()) {
		t.Fatal("fingerprint must be stable")
	}
}

func TestFingerprintOf_DiffersByAmount(t *testing.T) {
	other := request()
	other.Amount = money.New(101, money.BRL)
	if transfer.FingerprintOf(request()) == transfer.FingerprintOf(other) {
		t.Fatal("fingerprint must change with the amount")
	}
}

func TestReverse_Completed_MarksReversed(t *testing.T) {
	target := newTransfer(t)
	if err := target.Reverse(); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if target.Status() != transfer.StatusReversed {
		t.Fatalf("status = %s", target.Status())
	}
}

func TestReverse_Twice_ReturnsErrTransferAlreadyReversed(t *testing.T) {
	target := newTransfer(t)
	_ = target.Reverse()
	if err := target.Reverse(); !errors.Is(err, transfer.ErrTransferAlreadyReversed) {
		t.Fatalf("error = %v, want ErrTransferAlreadyReversed", err)
	}
}

func TestReverse_Reversal_ReturnsErrReversalNotReversible(t *testing.T) {
	reversal := transfer.Restore(transfer.Snapshot{Request: transfer.Request{Kind: transfer.KindReversal}, Status: transfer.StatusCompleted})
	if err := reversal.Reverse(); !errors.Is(err, transfer.ErrReversalNotReversible) {
		t.Fatalf("error = %v, want ErrReversalNotReversible", err)
	}
}

func TestReversalRequest_SwapsAccountsAndLinksOriginal(t *testing.T) {
	got, err := newTransfer(t).ReversalRequest("key-2")
	if err != nil {
		t.Fatalf("ReversalRequest: %v", err)
	}
	want := transfer.Request{
		OwnerID:    owner,
		Key:        "key-2",
		Kind:       transfer.KindReversal,
		From:       accountB,
		To:         accountA,
		Amount:     money.New(100, money.BRL),
		ReversalOf: "t1",
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReversalRequest_OnReversal_ReturnsErrReversalNotReversible(t *testing.T) {
	reversal := transfer.Restore(transfer.Snapshot{Request: transfer.Request{Kind: transfer.KindReversal}})
	if _, err := reversal.ReversalRequest("key-2"); !errors.Is(err, transfer.ErrReversalNotReversible) {
		t.Fatalf("error = %v, want ErrReversalNotReversible", err)
	}
}

func TestJournal_UsesTransferIDAndDirections(t *testing.T) {
	journal, err := newTransfer(t).Journal([2]string{"e1", "e2"})
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	entries := journal.Entries()
	if journal.ID() != "t1" || entries[0].AccountID() != accountA || entries[0].Direction() != ledger.Debit {
		t.Fatalf("got %+v", journal)
	}
	if entries[1].AccountID() != accountB || entries[1].Direction() != ledger.Credit {
		t.Fatalf("got %+v", entries[1])
	}
}
