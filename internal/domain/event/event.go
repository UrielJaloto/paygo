package event

import (
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const (
	TypeTransferCompleted = "transfer.completed"
	TypeTransferReversed  = "transfer.reversed"
	TypeChargePaid        = "charge.paid"
	TypeChargeExpired     = "charge.expired"
	TypePayoutCompleted   = "payout.completed"
	TypePayoutFailed      = "payout.failed"
)

type Metadata struct {
	ID string
	At time.Time
}

type Event struct {
	metadata   Metadata
	eventType  string
	transfer   transfer.Transfer
	charge     payment.Charge
	payout     payment.Payout
	transferID string
	ownerIDs   []apikey.OwnerID
}

func NewTransferEvent(metadata Metadata, transfer transfer.Transfer, owners []apikey.OwnerID) Event {
	return Event{metadata: metadata, eventType: resolveTransferType(transfer), transfer: transfer, transferID: string(transfer.ID()), ownerIDs: removeDuplicateRecipients(owners)}
}

func NewChargeEvent(metadata Metadata, eventType string, charge payment.Charge) Event {
	owners := removeDuplicateRecipients([]apikey.OwnerID{charge.OwnerID()})
	return Event{metadata: metadata, eventType: eventType, charge: charge, transferID: charge.TransferID(), ownerIDs: owners}
}

func NewPayoutEvent(metadata Metadata, eventType string, payout payment.Payout) Event {
	owners := removeDuplicateRecipients([]apikey.OwnerID{payout.OwnerID()})
	return Event{metadata: metadata, eventType: eventType, payout: payout, transferID: payout.Transfers().Final, ownerIDs: owners}
}

type Snapshot struct {
	Metadata   Metadata
	Type       string
	Transfer   transfer.Transfer
	TransferID string
	OwnerIDs   []apikey.OwnerID
}

func Restore(snapshot Snapshot) Event {
	return Event{
		metadata:   snapshot.Metadata,
		eventType:  snapshot.Type,
		transfer:   snapshot.Transfer,
		transferID: snapshot.TransferID,
		ownerIDs:   snapshot.OwnerIDs,
	}
}

func resolveTransferType(moved transfer.Transfer) string {
	if moved.Kind() == transfer.KindReversal {
		return TypeTransferReversed
	}
	return TypeTransferCompleted
}

func removeDuplicateRecipients(owners []apikey.OwnerID) []apikey.OwnerID {
	unique := make([]apikey.OwnerID, 0, len(owners))
	for _, owner := range owners {
		if owner != apikey.SystemOwnerID && !slices.Contains(unique, owner) {
			unique = append(unique, owner)
		}
	}
	return unique
}

func (e Event) ID() string {
	return e.metadata.ID
}

func (e Event) Type() string {
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
