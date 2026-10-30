package transfer

import (
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
)

const (
	KindTransfer   Kind = "transfer"
	KindDeposit    Kind = "deposit"
	KindWithdrawal Kind = "withdrawal"
	KindReversal   Kind = "reversal"

	KindCharge           Kind = "charge"
	KindPayout           Kind = "payout"
	KindPayoutCompletion Kind = "payout_completion"
	KindPayoutFailure    Kind = "payout_failure"

	StatusCompleted Status = "completed"
	StatusReversed  Status = "reversed"
)

var (
	ErrSameAccount             = errors.New("same account")
	ErrInsufficientFunds       = errors.New("insufficient funds")
	ErrTransferNotFound        = errors.New("transfer not found")
	ErrTransferAlreadyReversed = errors.New("transfer already reversed")
	ErrReversalNotReversible   = errors.New("reversal not reversible")
	ErrIdempotencyConflict     = errors.New("idempotency conflict")
	ErrInvalidIdempotencyKey   = errors.New("invalid idempotency key")
)

type ID string

type Kind string

type Status string

type Request struct {
	OwnerID    apikey.OwnerID
	Key        IdempotencyKey
	Kind       Kind
	From       account.ID
	To         account.ID
	Amount     money.Money
	ReversalOf ID
}

type Snapshot struct {
	ID        ID
	Request   Request
	Status    Status
	CreatedAt time.Time
}

type Transfer struct {
	id             ID
	ownerID        apikey.OwnerID
	idempotencyKey IdempotencyKey
	kind           Kind
	from           account.ID
	to             account.ID
	amount         money.Money
	status         Status
	reversalOf     ID
	createdAt      time.Time
}

func New(id ID, request Request, createdAt time.Time) (Transfer, error) {
	if request.From == request.To {
		return Transfer{}, fmt.Errorf("%w: %s", ErrSameAccount, request.From)
	}
	return Restore(Snapshot{ID: id, Request: request, Status: StatusCompleted, CreatedAt: createdAt}), nil
}

type Movement struct {
	OwnerID apikey.OwnerID
	From    account.ID
	To      account.ID
	Amount  money.Money
}

func (m Movement) request(kind Kind) Request {
	return Request{OwnerID: m.OwnerID, Kind: kind, From: m.From, To: m.To, Amount: m.Amount}
}

func NewCharge(id ID, movement Movement, createdAt time.Time) (Transfer, error) {
	return New(id, movement.request(KindCharge), createdAt)
}

func NewPayout(id ID, movement Movement, createdAt time.Time) (Transfer, error) {
	return New(id, movement.request(KindPayout), createdAt)
}

func NewPayoutCompletion(id ID, movement Movement, createdAt time.Time) (Transfer, error) {
	return New(id, movement.request(KindPayoutCompletion), createdAt)
}

func NewPayoutFailure(id ID, movement Movement, createdAt time.Time) (Transfer, error) {
	return New(id, movement.request(KindPayoutFailure), createdAt)
}

func Restore(snapshot Snapshot) Transfer {
	return Transfer{
		id:             snapshot.ID,
		ownerID:        snapshot.Request.OwnerID,
		idempotencyKey: snapshot.Request.Key,
		kind:           snapshot.Request.Kind,
		from:           snapshot.Request.From,
		to:             snapshot.Request.To,
		amount:         snapshot.Request.Amount,
		status:         snapshot.Status,
		reversalOf:     snapshot.Request.ReversalOf,
		createdAt:      snapshot.CreatedAt,
	}
}

func (t Transfer) ID() ID {
	return t.id
}

func (t Transfer) OwnerID() apikey.OwnerID {
	return t.ownerID
}

func (t Transfer) IdempotencyKey() IdempotencyKey {
	return t.idempotencyKey
}

func (t Transfer) Kind() Kind {
	return t.kind
}

func (t Transfer) From() account.ID {
	return t.from
}

func (t Transfer) To() account.ID {
	return t.to
}

func (t Transfer) Amount() money.Money {
	return t.amount
}

func (t Transfer) Status() Status {
	return t.status
}

func (t Transfer) ReversalOf() ID {
	return t.reversalOf
}

func (t Transfer) CreatedAt() time.Time {
	return t.createdAt
}

func (t *Transfer) Reverse() error {
	if t.kind == KindReversal {
		return ErrReversalNotReversible
	}
	if t.status == StatusReversed {
		return ErrTransferAlreadyReversed
	}
	t.status = StatusReversed
	return nil
}

func (t Transfer) ReversalRequest(key IdempotencyKey) (Request, error) {
	if t.kind == KindReversal {
		return Request{}, ErrReversalNotReversible
	}
	return Request{
		OwnerID:    t.ownerID,
		Key:        key,
		Kind:       KindReversal,
		From:       t.to,
		To:         t.from,
		Amount:     t.amount,
		ReversalOf: t.id,
	}, nil
}

func (t Transfer) Journal(entryIDs [2]string) (ledger.JournalTransaction, error) {
	return ledger.Transfer(ledger.Posting{
		ID:       string(t.id),
		EntryIDs: entryIDs,
		From:     t.from,
		To:       t.to,
		Amount:   t.amount,
		At:       t.createdAt,
	})
}
