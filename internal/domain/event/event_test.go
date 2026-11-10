package event_test

import (
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

var (
	occurredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	metadata   = event.Metadata{ID: "e1", At: occurredAt}
)

func ofKind(kind string) transfer.Transfer {
	return transfer.Restore(transfer.Snapshot{ID: "t1", Request: transfer.Request{Kind: kind}})
}

func TestNewTransferEvent_TypeFollowsKind(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{kind: transfer.KindTransfer, want: event.TypeTransferCompleted},
		{kind: transfer.KindDeposit, want: event.TypeTransferCompleted},
		{kind: transfer.KindWithdrawal, want: event.TypeTransferCompleted},
		{kind: transfer.KindReversal, want: event.TypeTransferReversed},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.kind), func(t *testing.T) {
			got := event.NewTransferEvent(metadata, ofKind(testCase.kind), nil)
			if got.Type() != testCase.want {
				t.Fatalf("type = %s, want %s", got.Type(), testCase.want)
			}
		})
	}
}

func TestNewTransferEvent_ExposesFields(t *testing.T) {
	got := event.NewTransferEvent(metadata, ofKind(transfer.KindTransfer), nil)
	if got.ID() != "e1" || got.Transfer().ID() != "t1" || got.TransferID() != "t1" || !got.OccurredAt().Equal(occurredAt) {
		t.Fatalf("got %+v", got)
	}
}

func TestNewChargeEvent_TargetsOwnerAndLinksTransfer(t *testing.T) {
	charge := payment.RestoreCharge(payment.ChargeSnapshot{ID: "c1", OwnerID: "o1", TransferID: "t1"})
	got := event.NewChargeEvent(metadata, event.TypeChargePaid, charge)
	if got.Type() != event.TypeChargePaid || got.Charge().ID() != "c1" || got.TransferID() != "t1" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got.OwnerIDs(), []apikey.OwnerID{"o1"}) {
		t.Fatalf("owners = %v", got.OwnerIDs())
	}
}

func TestNewPayoutEvent_TargetsOwnerAndLinksFinalTransfer(t *testing.T) {
	payout := payment.RestorePayout(payment.PayoutSnapshot{
		ID:        "p1",
		OwnerID:   "o1",
		Transfers: payment.PayoutTransfers{Hold: "hold", Final: "t1"},
	})
	got := event.NewPayoutEvent(metadata, event.TypePayoutCompleted, payout)
	if got.Type() != event.TypePayoutCompleted || got.Payout().ID() != "p1" || got.TransferID() != "t1" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got.OwnerIDs(), []apikey.OwnerID{"o1"}) {
		t.Fatalf("owners = %v", got.OwnerIDs())
	}
}

func TestOwnerIDs_DeduplicatesAndDropsSystemOwner(t *testing.T) {
	owners := []apikey.OwnerID{"o1", apikey.SystemOwnerID, "o1", "o2"}
	got := event.NewTransferEvent(metadata, ofKind(transfer.KindTransfer), owners).OwnerIDs()
	if !slices.Equal(got, []apikey.OwnerID{"o1", "o2"}) {
		t.Fatalf("owners = %v", got)
	}
}

func TestOwnerIDs_ReturnsCopy(t *testing.T) {
	created := event.NewTransferEvent(metadata, ofKind(transfer.KindTransfer), []apikey.OwnerID{"o1"})
	created.OwnerIDs()[0] = "changed"
	if created.OwnerIDs()[0] != "o1" {
		t.Fatalf("owners changed to %v", created.OwnerIDs())
	}
}

func TestRestore_KeepsEveryField(t *testing.T) {
	owners := []apikey.OwnerID{"o1", "o2"}
	got := event.Restore(event.Snapshot{Metadata: metadata, Type: event.TypeChargePaid, Transfer: ofKind(transfer.KindDeposit), TransferID: "t1", OwnerIDs: owners})
	if got.ID() != metadata.ID || got.OccurredAt() != occurredAt || got.Type() != event.TypeChargePaid {
		t.Fatalf("restored metadata = %s %v %s", got.ID(), got.OccurredAt(), got.Type())
	}
	if got.Transfer().ID() != "t1" || got.TransferID() != "t1" {
		t.Fatalf("restored transfer = %s, id = %s", got.Transfer().ID(), got.TransferID())
	}
	if !slices.Equal(got.OwnerIDs(), owners) {
		t.Fatalf("owners = %v, want %v", got.OwnerIDs(), owners)
	}
}
