package event_test

import (
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

var (
	occurredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	meta       = event.Meta{ID: "e1", At: occurredAt}
)

func ofKind(kind transfer.Kind) transfer.Transfer {
	return transfer.Restore(transfer.Snapshot{ID: "t1", Request: transfer.Request{Kind: kind}})
}

func TestForTransfer_TypeFollowsKind(t *testing.T) {
	cases := []struct {
		kind transfer.Kind
		want event.Type
	}{
		{kind: transfer.KindTransfer, want: event.TypeTransferCompleted},
		{kind: transfer.KindDeposit, want: event.TypeTransferCompleted},
		{kind: transfer.KindWithdrawal, want: event.TypeTransferCompleted},
		{kind: transfer.KindReversal, want: event.TypeTransferReversed},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			got := event.ForTransfer(meta, ofKind(tc.kind), nil)
			if got.Type() != tc.want {
				t.Fatalf("type = %s, want %s", got.Type(), tc.want)
			}
		})
	}
}

func TestForTransfer_ExposesFields(t *testing.T) {
	got := event.ForTransfer(meta, ofKind(transfer.KindTransfer), nil)
	if got.ID() != "e1" || got.Transfer().ID() != "t1" || got.TransferID() != "t1" || !got.OccurredAt().Equal(occurredAt) {
		t.Fatalf("got %+v", got)
	}
}

func TestForCharge_TargetsOwnerAndLinksTransfer(t *testing.T) {
	charge := payment.RestoreCharge(payment.ChargeSnapshot{ID: "c1", OwnerID: "o1", TransferID: "t1"})
	got := event.ForCharge(meta, event.TypeChargePaid, charge)
	if got.Type() != event.TypeChargePaid || got.Charge().ID() != "c1" || got.TransferID() != "t1" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got.OwnerIDs(), []apikey.OwnerID{"o1"}) {
		t.Fatalf("owners = %v", got.OwnerIDs())
	}
}

func TestForPayout_TargetsOwnerAndLinksFinalTransfer(t *testing.T) {
	payout := payment.RestorePayout(payment.PayoutSnapshot{
		ID:        "p1",
		OwnerID:   "o1",
		Transfers: payment.PayoutTransfers{Hold: "hold", Final: "t1"},
	})
	got := event.ForPayout(meta, event.TypePayoutCompleted, payout)
	if got.Type() != event.TypePayoutCompleted || got.Payout().ID() != "p1" || got.TransferID() != "t1" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got.OwnerIDs(), []apikey.OwnerID{"o1"}) {
		t.Fatalf("owners = %v", got.OwnerIDs())
	}
}

func TestOwnerIDs_DeduplicatesAndDropsSystemOwner(t *testing.T) {
	owners := []apikey.OwnerID{"o1", apikey.SystemOwnerID, "o1", "o2"}
	got := event.ForTransfer(meta, ofKind(transfer.KindTransfer), owners).OwnerIDs()
	if !slices.Equal(got, []apikey.OwnerID{"o1", "o2"}) {
		t.Fatalf("owners = %v", got)
	}
}

func TestOwnerIDs_ReturnsCopy(t *testing.T) {
	created := event.ForTransfer(meta, ofKind(transfer.KindTransfer), []apikey.OwnerID{"o1"})
	created.OwnerIDs()[0] = "changed"
	if created.OwnerIDs()[0] != "o1" {
		t.Fatalf("owners changed to %v", created.OwnerIDs())
	}
}

func TestRestore_KeepsEveryField(t *testing.T) {
	owners := []apikey.OwnerID{"o1", "o2"}
	got := event.Restore(event.Snapshot{Meta: meta, Type: event.TypeChargePaid, Transfer: ofKind(transfer.KindDeposit), TransferID: "t1", OwnerIDs: owners})
	if got.ID() != meta.ID || got.OccurredAt() != occurredAt || got.Type() != event.TypeChargePaid {
		t.Fatalf("restored meta = %s %v %s", got.ID(), got.OccurredAt(), got.Type())
	}
	if got.Transfer().ID() != "t1" || got.TransferID() != "t1" {
		t.Fatalf("restored transfer = %s, id = %s", got.Transfer().ID(), got.TransferID())
	}
	if !slices.Equal(got.OwnerIDs(), owners) {
		t.Fatalf("owners = %v, want %v", got.OwnerIDs(), owners)
	}
}
