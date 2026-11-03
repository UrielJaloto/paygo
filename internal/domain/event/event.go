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

type Meta struct {
	ID ID
	At time.Time
}

type Event struct {
	meta       Meta
	eventType  Type
	transfer   transfer.Transfer
	charge     payment.Charge
	payout     payment.Payout
	transferID string
	ownerIDs   []apikey.OwnerID
}

func ForTransfer(meta Meta, t transfer.Transfer, owners []apikey.OwnerID) Event {
	return Event{meta: meta, eventType: typeOf(t), transfer: t, transferID: string(t.ID()), ownerIDs: recipients(owners)}
}

func ForCharge(meta Meta, eventType Type, charge payment.Charge) Event {
	owners := recipients([]apikey.OwnerID{charge.OwnerID()})
	return Event{meta: meta, eventType: eventType, charge: charge, transferID: charge.TransferID(), ownerIDs: owners}
}

func ForPayout(meta Meta, eventType Type, payout payment.Payout) Event {
	owners := recipients([]apikey.OwnerID{payout.OwnerID()})
	return Event{meta: meta, eventType: eventType, payout: payout, transferID: payout.Transfers().Final, ownerIDs: owners}
}

type Snapshot struct {
	Meta       Meta
	Type       Type
	Transfer   transfer.Transfer
	TransferID string
	OwnerIDs   []apikey.OwnerID
}

func Restore(snapshot Snapshot) Event {
	return Event{
		meta:       snapshot.Meta,
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
	return e.meta.ID
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
	return e.meta.At
}
