package payment

import (
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

const (
	PayoutPending   = "pending"
	PayoutCompleted = "completed"
	PayoutFailed    = "failed"
)

type PayoutTransfers struct {
	Hold  string
	Final string
}

type PayoutDraft struct {
	OwnerID        apikey.OwnerID
	AccountID      account.ID
	Amount         money.Money
	PixKey         PixKey
	Provider       string
	HoldTransferID string
}

type PayoutSnapshot struct {
	ID        string
	OwnerID   apikey.OwnerID
	AccountID account.ID
	Amount    money.Money
	PixKey    PixKey
	Reference Reference
	Transfers PayoutTransfers
	Status    string
	Timeline  Timeline
}

type Payout struct {
	state PayoutSnapshot
}

func NewPayout(id string, draft PayoutDraft, createdAt time.Time) (Payout, error) {
	if draft.Amount.Currency() != money.BRL {
		return Payout{}, fmt.Errorf("%w: %s", money.ErrUnsupportedCurrency, draft.Amount.Currency())
	}
	return RestorePayout(PayoutSnapshot{
		ID:        id,
		OwnerID:   draft.OwnerID,
		AccountID: draft.AccountID,
		Amount:    draft.Amount,
		PixKey:    draft.PixKey,
		Reference: Reference{Provider: draft.Provider},
		Transfers: PayoutTransfers{Hold: draft.HoldTransferID},
		Status:    PayoutPending,
		Timeline:  Timeline{CreatedAt: createdAt, UpdatedAt: createdAt},
	}), nil
}

func RestorePayout(snapshot PayoutSnapshot) Payout {
	return Payout{state: snapshot}
}

func (p Payout) ID() string {
	return p.state.ID
}

func (p Payout) OwnerID() apikey.OwnerID {
	return p.state.OwnerID
}

func (p Payout) AccountID() account.ID {
	return p.state.AccountID
}

func (p Payout) Amount() money.Money {
	return p.state.Amount
}

func (p Payout) PixKey() PixKey {
	return p.state.PixKey
}

func (p Payout) Reference() Reference {
	return p.state.Reference
}

func (p Payout) Transfers() PayoutTransfers {
	return p.state.Transfers
}

func (p Payout) Status() string {
	return p.state.Status
}

func (p Payout) Timeline() Timeline {
	return p.state.Timeline
}

func (p *Payout) AttachProvider(providerID string) {
	p.state.Reference.ProviderID = providerID
}

func (p *Payout) MarkCompleted(transferID string, now time.Time) (bool, error) {
	return p.changeStatus(PayoutCompleted, transferID, now)
}

func (p *Payout) MarkFailed(transferID string, now time.Time) (bool, error) {
	return p.changeStatus(PayoutFailed, transferID, now)
}

func (p *Payout) changeStatus(target string, transferID string, now time.Time) (bool, error) {
	if p.state.Status == target {
		return false, nil
	}
	if p.state.Status != PayoutPending {
		return false, fmt.Errorf("%w: payout %s to %s", ErrInvalidTransition, p.state.Status, target)
	}
	p.state.Status = target
	p.state.Transfers.Final = transferID
	p.state.Timeline.UpdatedAt = now
	return true, nil
}
