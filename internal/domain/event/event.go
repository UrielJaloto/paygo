package event

import (
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const (
	TypeTransferCompleted Type = "transfer.completed"
	TypeTransferReversed  Type = "transfer.reversed"
	TypeChargePaid        Type = "charge.paid"
	TypeChargeExpired     Type = "charge.expired"
	TypePayoutCompleted   Type = "payout.completed"
	TypePayoutFailed      Type = "payout.failed"
)

type ID string

type Type string

type Metadata struct {
	ID ID
	At time.Time
}

type Event struct {
	metadata       Metadata
	eventType  Type
	transfer   transfer.Transfer
	charge     payment.Charge
	payout     payment.Payout
	transferID string
	ownerIDs   []apikey.OwnerID
}

func NewTransferEvent(metadata Metadata, t transfer.Transfer, owners []apikey.OwnerID) Event {
	return Event{metadata: metadata, eventType: typeOf(t), transfer: t, transferID: string(t.ID()), ownerIDs: recipients(owners)}
}

func NewChargeEvent(metadata Metadata, eventType Type, charge payment.Charge) Event {
	owners := recipients([]apikey.OwnerID{charge.OwnerID()})
	return Event{metadata: metadata, eventType: eventType, charge: charge, transferID: charge.TransferID(), ownerIDs: owners}
}

func NewPayoutEvent(metadata Metadata, eventType Type, payout payment.Payout) Event {
	owners := recipients([]apikey.OwnerID{payout.OwnerID()})
	return Event{metadata: metadata, eventType: eventType, payout: payout, transferID: payout.Transfers().Final, ownerIDs: owners}
}

type Snapshot struct {
	Metadata       Metadata
	Type       Type
	Transfer   transfer.Transfer
	TransferID string
	OwnerIDs   []apikey.OwnerID
}

func Restore(snapshot Snapshot) Event {
	return Event{
		metadata:       snapshot.Metadata,
		eventType:  snapshot.Type,
		transfer:   snapshot.Transfer,
		transferID: snapshot.TransferID,
		ownerIDs:   snapshot.OwnerIDs,
	}
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
	return e.metadata.ID
}

func (e Event) Type() Type {
	return e.eventType
}

func (e Event) Transfer() transfer.Transfer {
	return e.transfer
}

func (e Event) Charge() payment.Charge {
	return e.charge
}

func (e Event) Payout() payment.Payout {
	return e.payout
}

func (e Event) TransferID() string {
	return e.transferID
}

func (e Event) OwnerIDs() []apikey.OwnerID {
	return slices.Clone(e.ownerIDs)
}

func (e Event) OccurredAt() time.Time {
	return e.metadata.At
}
