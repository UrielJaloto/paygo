package event

import (
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

const (
	TypeTransferCompleted Type = "transfer.completed"
	TypeTransferReversed  Type = "transfer.reversed"
)

type ID string

type Type string

type Event struct {
	id         ID
	eventType  Type
	transfer   transfer.Transfer
	ownerIDs   []apikey.OwnerID
	occurredAt time.Time
}

func NewTransferEvent(id ID, t transfer.Transfer, owners []apikey.OwnerID, at time.Time) Event {
	return Event{id: id, eventType: typeOf(t), transfer: t, ownerIDs: recipients(owners), occurredAt: at}
}

func typeOf(t transfer.Transfer) Type {
	if t.Kind() == transfer.KindReversal {
		return TypeTransferReversed
	}
	return TypeTransferCompleted
}

func recipients(owners []apikey.OwnerID) []apikey.OwnerID {
	unique := make([]apikey.OwnerID, 0, len(owners))
	for _, owner := range owners {
		if owner != apikey.SystemOwnerID && !slices.Contains(unique, owner) {
			unique = append(unique, owner)
		}
	}
	return unique
}

func (e Event) ID() ID {
	return e.id
}

func (e Event) Type() Type {
	return e.eventType
}

func (e Event) Transfer() transfer.Transfer {
	return e.transfer
}

func (e Event) OwnerIDs() []apikey.OwnerID {
	return slices.Clone(e.ownerIDs)
}

func (e Event) OccurredAt() time.Time {
	return e.occurredAt
}
