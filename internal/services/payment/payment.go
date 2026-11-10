package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

const chargeLifetime = 24 * time.Hour

type UnitOfWork interface {
	Run(ctx context.Context, work func(ctx context.Context, store Store) error) error
}

type Store struct {
	Accounts  AccountLocker
	Ledger    Ledger
	Transfers TransferWriter
	Charges   ChargeRepository
	Payouts   PayoutRepository
	Events    ProviderEventLog
	Outbox    Outbox
}

type AccountLocker interface {
	LockByIDs(ctx context.Context, ids []account.ID) ([]account.Account, error)
}

type Ledger interface {
	FindBalance(ctx context.Context, id account.ID) (money.Money, error)
	Append(ctx context.Context, journal ledger.JournalTransaction) error
}

type TransferWriter interface {
	Save(ctx context.Context, t transfer.Transfer, fingerprint string) error
}

type ChargeRepository interface {
	Save(ctx context.Context, charge payment.Charge, key transfer.IdempotencyKey, fingerprint string) error
	Update(ctx context.Context, charge payment.Charge) error
	FindByID(ctx context.Context, id string) (payment.Charge, error)
	FindByProviderID(ctx context.Context, providerID string) (payment.Charge, error)
	FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (payment.Charge, string, error)
	ListPending(ctx context.Context, olderThan time.Time, limit int) ([]payment.Charge, error)
}

type PayoutRepository interface {
	Save(ctx context.Context, payout payment.Payout, key transfer.IdempotencyKey, fingerprint string) error
	Update(ctx context.Context, payout payment.Payout) error
	FindByID(ctx context.Context, id string) (payment.Payout, error)
	FindByProviderID(ctx context.Context, providerID string) (payment.Payout, error)
	FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (payment.Payout, string, error)
	ListPending(ctx context.Context, olderThan time.Time, limit int) ([]payment.Payout, error)
}

type ProviderEventLog interface {
	Record(ctx context.Context, provider, eventID string) (bool, error)
}

type Outbox interface {
	Add(ctx context.Context, e event.Event) error
}

type IDGenerator interface {
	NewID() string
}

type Service struct {
	unitOfWork UnitOfWork
	provider   PaymentProvider
	now        func() time.Time
	ids        IDGenerator
}

type chargeOpening struct {
	charge      payment.Charge
	key         transfer.IdempotencyKey
	fingerprint string
}

func NewService(unitOfWork UnitOfWork, provider PaymentProvider, now func() time.Time, ids IDGenerator) *Service {
	return &Service{unitOfWork: unitOfWork, provider: provider, now: now, ids: ids}
}

func (s *Service) CreateCharge(ctx context.Context, input ChargeInput) (ChargeOutput, error) {
	opening, err := s.parseCharge(input)
	if err != nil {
		return ChargeOutput{}, err
	}
	replayed, found, err := s.openCharge(ctx, opening)
	if err != nil || found {
		return replayed, err
	}
	return s.requestPix(ctx, opening.charge)
}

func (s *Service) FindCharge(ctx context.Context, query ChargeQuery) (ChargeOutput, error) {
	owner, err := apikey.ParseOwnerID(query.OwnerID)
	if err != nil {
		return ChargeOutput{}, err
	}
	var found payment.Charge
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		var err error
		found, err = store.Charges.FindByID(ctx, query.ChargeID)
		return err
	})
	if err != nil {
		return ChargeOutput{}, fmt.Errorf("getting charge: %w", err)
	}
	if found.OwnerID() != owner {
		return ChargeOutput{}, fmt.Errorf("%w: %s", payment.ErrChargeNotFound, query.ChargeID)
	}
	return toChargeOutput(found), nil
}

func (s *Service) parseCharge(input ChargeInput) (chargeOpening, error) {
	key, err := transfer.ParseIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return chargeOpening{}, err
	}
	draft, err := s.parseChargeDraft(input)
	if err != nil {
		return chargeOpening{}, err
	}
	now := s.now()
	charge, err := payment.NewCharge(s.ids.NewID(), draft, now.Add(chargeLifetime), now)
	if err != nil {
		return chargeOpening{}, err
	}
	fingerprint := transfer.Request{
		Kind:   transfer.KindCharge,
		From:   findClearingAccountID(draft.Amount.Currency()),
		To:     draft.AccountID,
		Amount: draft.Amount,
	}.CalculateFingerprint()
	return chargeOpening{charge: charge, key: key, fingerprint: fingerprint}, nil
}

func (s *Service) parseChargeDraft(input ChargeInput) (payment.ChargeDraft, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return payment.ChargeDraft{}, err
	}
	id, err := account.ParseID(input.AccountID)
	if err != nil {
		return payment.ChargeDraft{}, err
	}
	amount, err := parseAmount(input.Amount, input.Currency)
	if err != nil {
		return payment.ChargeDraft{}, err
	}
	document, err := payment.ParseDocument(input.PayerDocument)
	if err != nil {
		return payment.ChargeDraft{}, err
	}
	return payment.ChargeDraft{
		OwnerID:   owner,
		AccountID: id,
		Amount:    amount,
		Payer:     payment.Payer{Document: document, Name: input.PayerName},
		Provider:  s.provider.Name(),
	}, nil
}

func (s *Service) openCharge(ctx context.Context, opening chargeOpening) (ChargeOutput, bool, error) {
	var replayed ChargeOutput
	var found bool
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		var err error
		replayed, found, err = findChargeReplay(ctx, store, opening)
		if err != nil || found {
			return err
		}
		if err := validateCustomerAccount(ctx, store, opening.charge.OwnerID(), opening.charge.AccountID()); err != nil {
			return err
		}
		replayed, found, err = findChargeReplay(ctx, store, opening)
		if err != nil || found {
			return err
		}
		return store.Charges.Save(ctx, opening.charge, opening.key, opening.fingerprint)
	})
	if err != nil {
		return ChargeOutput{}, false, fmt.Errorf("opening charge: %w", err)
	}
	return replayed, found, nil
}

func findChargeReplay(ctx context.Context, store Store, opening chargeOpening) (ChargeOutput, bool, error) {
	existing, fingerprint, err := store.Charges.FindByIdempotencyKey(ctx, opening.charge.OwnerID(), opening.key)
	if errors.Is(err, payment.ErrChargeNotFound) {
		return ChargeOutput{}, false, nil
	}
	if err != nil {
		return ChargeOutput{}, false, fmt.Errorf("finding idempotency key: %w", err)
	}
	if fingerprint != opening.fingerprint {
		return ChargeOutput{}, false, fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, opening.key)
	}
	output := toChargeOutput(existing)
	output.IsReplay = true
	return output, true, nil
}

func validateCustomerAccount(ctx context.Context, store Store, owner apikey.OwnerID, id account.ID) error {
	locked, err := store.Accounts.LockByIDs(ctx, []account.ID{id})
	if err != nil {
		return fmt.Errorf("locking account: %w", err)
	}
	found := locked[0]
	if found.Kind() != account.KindCustomer || !found.IsOwnedBy(owner) || found.Currency() != money.BRL {
		return fmt.Errorf("%w: %s", account.ErrAccountNotFound, id)
	}
	return nil
}

func (s *Service) requestPix(ctx context.Context, charge payment.Charge) (ChargeOutput, error) {
	result, err := s.provider.CreatePixCharge(ctx, ChargeRequest{
		ExternalReference: string(charge.ID()),
		PayerName:         charge.Payer().Name,
		PayerDocument:     string(charge.Payer().Document),
		AmountMinor:       charge.Amount().Minor(),
		DueDate:           charge.Pix().ExpiresAt,
	})
	if err != nil {
		return ChargeOutput{}, s.markChargeFailed(ctx, charge, err)
	}
	charge.AttachProvider(result.ProviderID, result.PixCopyPaste, result.PixQRBase64)
	if err := s.updateCharge(ctx, charge); err != nil {
		return ChargeOutput{}, err
	}
	return toChargeOutput(charge), nil
}

func (s *Service) markChargeFailed(ctx context.Context, charge payment.Charge, cause error) error {
	transition := charge.MarkFailed(s.now())
	return fmt.Errorf("creating pix charge: %w", errors.Join(cause, transition, s.updateCharge(ctx, charge)))
}

func (s *Service) updateCharge(ctx context.Context, charge payment.Charge) error {
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		return store.Charges.Update(ctx, charge)
	})
	if err != nil {
		return fmt.Errorf("updating charge: %w", err)
	}
	return nil
}

func parseAmount(amount, currencyCode string) (money.Money, error) {
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return money.Money{}, err
	}
	return money.ParseAmount(amount, currency)
}

func toChargeOutput(charge payment.Charge) ChargeOutput {
	return ChargeOutput{
		ID:           string(charge.ID()),
		AccountID:    string(charge.AccountID()),
		Status:       string(charge.Status()),
		PixCopyPaste: charge.Pix().CopyPaste,
		PixQRBase64:  charge.Pix().QRBase64,
		AmountMinor:  charge.Amount().Minor(),
		Currency:     string(charge.Amount().Currency()),
		ExpiresAt:    charge.Pix().ExpiresAt,
		CreatedAt:    charge.Timeline().CreatedAt,
	}
}
