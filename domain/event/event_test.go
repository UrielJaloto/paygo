package event_test

import (
	"slices"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

var occurredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func ofKind(kind transfer.Kind) transfer.Transfer {
	return transfer.Restore(transfer.Snapshot{ID: "t1", Request: transfer.Request{Kind: kind}})
}

func TestNewTransferEvent_TypeFollowsKind(t *testing.T) {
	cases := []struct {
		kind transfer.Kind
		want event.Type
	}{
		{kind: transfer.KindTransfer, want: event.TypeTransferCompleted},
		{kind: transfer.KindDeposit, want: event.TypeTransferCompleted},
		{kind: transfer.KindWithdrawal, want: event.TypeTransferCompleted},
		{kind: transfer.KindReversal, want: event.TypeTransferReversed},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.kind), func(t *testing.T) {
			got := event.NewTransferEvent("e1", ofKind(testCase.kind), nil, occurredAt)
			if got.Type() != testCase.want {
				t.Fatalf("type = %s, want %s", got.Type(), testCase.want)
			}
		})
	}
}

func TestNewTransferEvent_ExposesFields(t *testing.T) {
	got := event.NewTransferEvent("e1", ofKind(transfer.KindTransfer), nil, occurredAt)
	if got.ID() != "e1" || got.Transfer().ID() != "t1" || !got.OccurredAt().Equal(occurredAt) {
		t.Fatalf("got %+v", got)
	}
}

func TestOwnerIDs_DeduplicatesAndDropsSystemOwner(t *testing.T) {
	owners := []apikey.OwnerID{"o1", apikey.SystemOwnerID, "o1", "o2"}
	got := event.NewTransferEvent("e1", ofKind(transfer.KindTransfer), owners, occurredAt).OwnerIDs()
	if !slices.Equal(got, []apikey.OwnerID{"o1", "o2"}) {
		t.Fatalf("owners = %v", got)
	}
}

func TestOwnerIDs_ReturnsCopy(t *testing.T) {
	created := event.NewTransferEvent("e1", ofKind(transfer.KindTransfer), []apikey.OwnerID{"o1"}, occurredAt)
	created.OwnerIDs()[0] = "changed"
	if created.OwnerIDs()[0] != "o1" {
		t.Fatalf("owners changed to %v", created.OwnerIDs())
	}
}
