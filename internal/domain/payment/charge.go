package payment

import (
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

const (
	ChargePending = "pending"
	ChargePaid    = "paid"
	ChargeExpired = "expired"
	ChargeFailed  = "failed"

	maximumPayerNameLength = 100
)

type Payer struct {
	Document Document
	Name     string
}

type Reference struct {
	Provider   string
	ProviderID string
}

type PixCode struct {
	CopyPaste string
	QRBase64  string
	ExpiresAt time.Time
}

type Timeline struct {
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ChargeDraft struct {
	OwnerID   apikey.OwnerID
	AccountID account.ID
	Amount    money.Money
	Payer     Payer
	Provider  string
}

type ChargeSnapshot struct {
	ID         string
	OwnerID    apikey.OwnerID
	AccountID  account.ID
	Amount     money.Money
	Payer      Payer
	Reference  Reference
	Pix        PixCode
	Status     string
	TransferID string
	Timeline   Timeline
}

type Charge struct {
	state ChargeSnapshot
}

func NewCharge(id string, draft ChargeDraft, expiresAt, createdAt time.Time) (Charge, error) {
	if draft.Amount.Currency() != money.BRL {
		return Charge{}, fmt.Errorf("%w: %s", money.ErrUnsupportedCurrency, draft.Amount.Currency())
	}
	if length := len([]rune(draft.Payer.Name)); length < 1 || length > maximumPayerNameLength {
		return Charge{}, fmt.Errorf("%w: %d characters", ErrInvalidPayerName, length)
	}
	return RestoreCharge(ChargeSnapshot{
		ID:        id,
		OwnerID:   draft.OwnerID,
		AccountID: draft.AccountID,
		Amount:    draft.Amount,
		Payer:     draft.Payer,
		Reference: Reference{Provider: draft.Provider},
		Pix:       PixCode{ExpiresAt: expiresAt},
		Status:    ChargePending,
		Timeline:  Timeline{CreatedAt: createdAt, UpdatedAt: createdAt},
	}), nil
}

func RestoreCharge(snapshot ChargeSnapshot) Charge {
	return Charge{state: snapshot}
}

func (c Charge) ID() string {
	return c.state.ID
}

func (c Charge) OwnerID() apikey.OwnerID {
	return c.state.OwnerID
}

func (c Charge) AccountID() account.ID {
	return c.state.AccountID
}

func (c Charge) Amount() money.Money {
	return c.state.Amount
}

func (c Charge) Payer() Payer {
	return c.state.Payer
}

func (c Charge) Reference() Reference {
	return c.state.Reference
}

func (c Charge) Pix() PixCode {
	return c.state.Pix
}

func (c Charge) Status() string {
	return c.state.Status
}

func (c Charge) TransferID() string {
	return c.state.TransferID
}

func (c Charge) Timeline() Timeline {
	return c.state.Timeline
}

func (c *Charge) AttachProvider(providerID, copyPaste, qrCode string) {
	c.state.Reference.ProviderID = providerID
	c.state.Pix.CopyPaste = copyPaste
	c.state.Pix.QRBase64 = qrCode
}

func (c *Charge) MarkPaid(transferID string, now time.Time) (bool, error) {
	if c.state.Status == ChargePaid {
		return false, nil
	}
	if err := c.changePendingStatus(ChargePaid, now); err != nil {
		return false, err
	}
	c.state.TransferID = transferID
	return true, nil
}

func (c *Charge) MarkExpired(now time.Time) error {
	return c.changePendingStatus(ChargeExpired, now)
}

func (c *Charge) MarkFailed(now time.Time) error {
	return c.changePendingStatus(ChargeFailed, now)
}

func (c *Charge) changePendingStatus(target string, now time.Time) error {
	if c.state.Status != ChargePending {
		return fmt.Errorf("%w: charge %s to %s", ErrInvalidTransition, c.state.Status, target)
	}
	c.state.Status = target
	c.state.Timeline.UpdatedAt = now
	return nil
}
