package account

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

const (
	KindCustomer         = "customer"
	KindSettlement       = "settlement"
	KindProviderClearing = "provider_clearing"
	KindPayoutHold       = "payout_hold"

	StatusActive = "active"
	StatusFrozen = "frozen"

	initialVersion int64 = 1
)

var (
	ErrInvalidAccountID       = errors.New("invalid account id")
	ErrAccountNotFound        = errors.New("account not found")
	ErrAccountFrozen          = errors.New("account frozen")
	ErrAccountAlreadyFrozen   = errors.New("account already frozen")
	ErrAccountNotFrozen       = errors.New("account not frozen")
	ErrConcurrentModification = errors.New("concurrent modification")
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type ID string

func ParseID(text string) (ID, error) {
	if !idPattern.MatchString(text) {
		return "", fmt.Errorf("%w: %q", ErrInvalidAccountID, text)
	}
	return ID(text), nil
}

type Snapshot struct {
	ID        ID
	OwnerID   apikey.OwnerID
	Currency  money.Currency
	Kind      string
	Status    string
	Version   int64
	CreatedAt time.Time
}

type Account struct {
	id        ID
	ownerID   apikey.OwnerID
	currency  money.Currency
	kind      string
	status    string
	version   int64
	createdAt time.Time
}

func NewCustomer(id ID, owner apikey.OwnerID, currency money.Currency, createdAt time.Time) (Account, error) {
	if owner == apikey.SystemOwnerID {
		return Account{}, fmt.Errorf("%w: system owner cannot hold customer accounts", apikey.ErrInvalidOwner)
	}
	return Restore(Snapshot{
		ID:        id,
		OwnerID:   owner,
		Currency:  currency,
		Kind:      KindCustomer,
		Status:    StatusActive,
		Version:   initialVersion,
		CreatedAt: createdAt,
	}), nil
}

func Restore(snapshot Snapshot) Account {
	return Account{
		id:        snapshot.ID,
		ownerID:   snapshot.OwnerID,
		currency:  snapshot.Currency,
		kind:      snapshot.Kind,
		status:    snapshot.Status,
		version:   snapshot.Version,
		createdAt: snapshot.CreatedAt,
	}
}

func (a Account) ID() ID {
	return a.id
}

func (a Account) OwnerID() apikey.OwnerID {
	return a.ownerID
}

func (a Account) Currency() money.Currency {
	return a.currency
}

func (a Account) Kind() string {
	return a.kind
}

func (a Account) Status() string {
	return a.status
}

func (a Account) Version() int64 {
	return a.version
}

func (a Account) CreatedAt() time.Time {
	return a.createdAt
}

func (a *Account) Freeze() error {
	if a.status == StatusFrozen {
		return ErrAccountAlreadyFrozen
	}
	a.status = StatusFrozen
	a.version++
	return nil
}

func (a *Account) Unfreeze() error {
	if a.status != StatusFrozen {
		return ErrAccountNotFrozen
	}
	a.status = StatusActive
	a.version++
	return nil
}

func (a Account) CanMoveMoney() bool {
	return a.status == StatusActive
}

func (a Account) AllowsNegativeBalance() bool {
	return a.kind == KindSettlement || a.kind == KindProviderClearing
}

func (a Account) IsOwnedBy(owner apikey.OwnerID) bool {
	return a.ownerID == owner
}
