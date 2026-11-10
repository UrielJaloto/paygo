package transfer

import (
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

const (
	KindTransfer   = "transfer"
	KindDeposit    = "deposit"
	KindWithdrawal = "withdrawal"
	KindReversal   = "reversal"

	KindCharge           = "charge"
	KindPayout           = "payout"
	KindPayoutCompletion = "payout_completion"
	KindPayoutFailure    = "payout_failure"

	StatusCompleted = "completed"
	StatusReversed  = "reversed"
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

type Request struct {
	OwnerID    apikey.OwnerID
	Key        IdempotencyKey
	Kind       string
	From       account.ID
	To         account.ID
	Amount     money.Money
	ReversalOf string
}

type Snapshot struct {
	ID        string
	Request   Request
	Status    string
	CreatedAt time.Time
}

type Transfer struct {
	id             string
	ownerID        apikey.OwnerID
	idempotencyKey IdempotencyKey
	kind           string
	from           account.ID
	to             account.ID
	amount         money.Money
	status         string
	reversalOf     string
	createdAt      time.Time
}

func NewTransfer(id string, request Request, createdAt time.Time) (Transfer, error) {
	if request.From == request.To {
		return Transfer{}, fmt.Errorf("%w: %s", ErrSameAccount, request.From)
	}
	return Restore(Snapshot{ID: id, Request: request, Status: StatusCompleted, CreatedAt: createdAt}), nil
}

type Movement struct {
	OwnerID apikey.OwnerID
	Key     IdempotencyKey
	From    account.ID
	To      account.ID
	Amount  money.Money
}

func (m Movement) toRequest(kind string) Request {
	return Request{OwnerID: m.OwnerID, Key: m.Key, Kind: kind, From: m.From, To: m.To, Amount: m.Amount}
}

func NewCharge(id string, movement Movement, createdAt time.Time) (Transfer, error) {
	return NewTransfer(id, movement.toRequest(KindCharge), createdAt)
}

func NewPayout(id string, movement Movement, createdAt time.Time) (Transfer, error) {
	return NewTransfer(id, movement.toRequest(KindPayout), createdAt)
}

func NewPayoutCompletion(id string, movement Movement, createdAt time.Time) (Transfer, error) {
	return NewTransfer(id, movement.toRequest(KindPayoutCompletion), createdAt)
}

func NewPayoutFailure(id string, movement Movement, createdAt time.Time) (Transfer, error) {
	return NewTransfer(id, movement.toRequest(KindPayoutFailure), createdAt)
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

func (t Transfer) ID() string {
	return t.id
}

func (t Transfer) OwnerID() apikey.OwnerID {
	return t.ownerID
}

func (t Transfer) IdempotencyKey() IdempotencyKey {
	return t.idempotencyKey
}

func (t Transfer) Kind() string {
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

func (t Transfer) Status() string {
	return t.status
}

func (t Transfer) ReversalOf() string {
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

func (t Transfer) BuildReversalRequest(key IdempotencyKey) (Request, error) {
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

func (t Transfer) BuildJournal(entryIDs [2]string) (ledger.JournalTransaction, error) {
	return ledger.Posting{
		ID:       string(t.id),
		EntryIDs: entryIDs,
		From:     t.from,
		To:       t.to,
		Amount:   t.amount,
		At:       t.createdAt,
	}.BuildJournal()
}
