package payment

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type payoutOpening struct {
	payout      payment.Payout
	key         transfer.IdempotencyKey
	fingerprint transfer.Fingerprint
}

type payoutSettlement struct {
	payout    payment.Payout
	plan      posting
	eventType event.Type
}

func (s *Service) CreatePayout(ctx context.Context, input PayoutInput) (PayoutOutput, error) {
	opening, err := s.parsePayout(input)
	if err != nil {
		return PayoutOutput{}, err
	}
	replayed, found, err := s.openPayout(ctx, opening)
	if err != nil || found {
		return replayed, err
	}
	sent, err := s.sendPayout(ctx, opening.payout)
	if errors.Is(err, payment.ErrProviderUnavailable) {
		return toPayoutOutput(opening.payout), nil
	}
	if err != nil {
		return PayoutOutput{}, err
	}
	return toPayoutOutput(sent), nil
}

func (s *Service) GetPayout(ctx context.Context, query PayoutQuery) (PayoutOutput, error) {
	owner, err := apikey.ParseOwnerID(query.OwnerID)
	if err != nil {
		return PayoutOutput{}, err
	}
	var found payment.Payout
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		var err error
		found, err = store.Payouts.FindByID(ctx, payment.PayoutID(query.PayoutID))
		return err
	})
	if err != nil {
		return PayoutOutput{}, fmt.Errorf("getting payout: %w", err)
	}
	if found.OwnerID() != owner {
		return PayoutOutput{}, fmt.Errorf("%w: %s", payment.ErrPayoutNotFound, query.PayoutID)
	}
	return toPayoutOutput(found), nil
}

func (s *Service) parsePayout(input PayoutInput) (payoutOpening, error) {
	key, err := transfer.ParseIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return payoutOpening{}, err
	}
	draft, err := s.parsePayoutDraft(input)
	if err != nil {
		return payoutOpening{}, err
	}
	created, err := payment.NewPayout(payment.PayoutID(s.ids.NewID()), draft, s.clock.Now())
	if err != nil {
		return payoutOpening{}, err
	}
	fingerprint := transfer.Request{
		Kind:   transfer.KindPayout,
		From:   draft.AccountID,
		To:     holdAccountFor(draft.Amount.Currency()),
		Amount: draft.Amount,
	}.CalculateFingerprint()
	return payoutOpening{payout: created, key: key, fingerprint: fingerprint}, nil
}

func (s *Service) parsePayoutDraft(input PayoutInput) (payment.PayoutDraft, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return payment.PayoutDraft{}, err
	}
	id, err := account.ParseID(input.AccountID)
	if err != nil {
		return payment.PayoutDraft{}, err
	}
	amount, err := parseAmount(input.Amount, input.Currency)
	if err != nil {
		return payment.PayoutDraft{}, err
	}
	pixKey, err := payment.ParsePixKey(input.PixKeyType, input.PixKey)
	if err != nil {
		return payment.PayoutDraft{}, err
	}
	return payment.PayoutDraft{
		OwnerID:        owner,
		AccountID:      id,
		Amount:         amount,
		PixKey:         pixKey,
		Provider:       s.provider.Name(),
		HoldTransferID: s.ids.NewID(),
	}, nil
}

func (s *Service) openPayout(ctx context.Context, opening payoutOpening) (PayoutOutput, bool, error) {
	var replayed PayoutOutput
	var found bool
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		var err error
		replayed, found, err = findPayoutReplay(ctx, store, opening)
		if err != nil || found {
			return err
		}
		return s.holdFunds(ctx, store, opening)
	})
	if err != nil {
		return PayoutOutput{}, false, fmt.Errorf("opening payout: %w", err)
	}
	return replayed, found, nil
}

func findPayoutReplay(ctx context.Context, store Store, opening payoutOpening) (PayoutOutput, bool, error) {
	existing, fingerprint, err := store.Payouts.FindByIdempotencyKey(ctx, opening.payout.OwnerID(), opening.key)
	if errors.Is(err, payment.ErrPayoutNotFound) {
		return PayoutOutput{}, false, nil
	}
	if err != nil {
		return PayoutOutput{}, false, fmt.Errorf("finding idempotency key: %w", err)
	}
	if fingerprint != opening.fingerprint {
		return PayoutOutput{}, false, fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, opening.key)
	}
	output := toPayoutOutput(existing)
	output.IsReplay = true
	return output, true, nil
}

func (s *Service) holdFunds(ctx context.Context, store Store, opening payoutOpening) error {
	created := opening.payout
	hold := holdAccountFor(created.Amount().Currency())
	if err := validatePayoutFunds(ctx, store, created, hold); err != nil {
		return err
	}
	err := s.recordPosting(ctx, store, posting{id: created.Transfers().Hold, build: transfer.NewPayout, movement: transfer.Movement{
		OwnerID: created.OwnerID(),
		Key:     transfer.IdempotencyKey("payout-" + string(created.ID())),
		From:    created.AccountID(),
		To:      hold,
		Amount:  created.Amount(),
	}})
	if err != nil {
		return err
	}
	return store.Payouts.Save(ctx, created, opening.key, opening.fingerprint)
}

func validatePayoutFunds(ctx context.Context, store Store, created payment.Payout, hold account.ID) error {
	id := created.AccountID()
	ids := []account.ID{id, hold}
	slices.Sort(ids)
	locked, err := store.Accounts.LockByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("locking accounts: %w", err)
	}
	customer := locked[slices.IndexFunc(locked, func(a account.Account) bool { return a.ID() == id })]
	if customer.Kind() != account.KindCustomer || !customer.IsOwnedBy(created.OwnerID()) || customer.Currency() != created.Amount().Currency() {
		return fmt.Errorf("%w: %s", account.ErrAccountNotFound, id)
	}
	if !customer.CanMoveMoney() {
		return fmt.Errorf("%w: %s", account.ErrAccountFrozen, id)
	}
	balance, err := store.Ledger.CalculateBalance(ctx, id)
	if err != nil {
		return fmt.Errorf("reading balance: %w", err)
	}
	remaining, err := balance.Subtract(created.Amount())
	if err != nil {
		return err
	}
	if remaining.IsNegative() {
		return fmt.Errorf("%w: %s", transfer.ErrInsufficientFunds, id)
	}
	return nil
}

func (s *Service) sendPayout(ctx context.Context, pending payment.Payout) (payment.Payout, error) {
	result, err := s.provider.SendPixPayout(ctx, PayoutRequest{
		ExternalReference: string(pending.ID()),
		PixKey:            pending.PixKey().Value(),
		PixKeyType:        string(pending.PixKey().Kind()),
		AmountMinor:       pending.Amount().Minor(),
	})
	if errors.Is(err, payment.ErrProviderRejected) || errors.Is(err, payment.ErrInvalidPixKey) {
		return payment.Payout{}, s.rejectPayout(ctx, pending, err)
	}
	if err != nil {
		return payment.Payout{}, fmt.Errorf("sending pix payout: %w", err)
	}
	pending.AttachProvider(result.ProviderID)
	err = s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		return store.Payouts.Update(ctx, pending)
	})
	if err != nil {
		return payment.Payout{}, fmt.Errorf("updating payout: %w", err)
	}
	return pending, nil
}

func (s *Service) rejectPayout(ctx context.Context, pending payment.Payout, cause error) error {
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		return s.markPayoutFailed(ctx, store, pending)
	})
	return fmt.Errorf("sending pix payout: %w", errors.Join(cause, err))
}

func (s *Service) applyPayoutEvent(ctx context.Context, store Store, occurred payment.ProviderEvent) error {
	found, err := store.Payouts.FindByProviderID(ctx, occurred.ProviderID)
	if err != nil {
		return fmt.Errorf("finding payout: %w", err)
	}
	if occurred.Kind == payment.EventPayoutDone {
		return s.markPayoutCompleted(ctx, store, found)
	}
	return s.markPayoutFailed(ctx, store, found)
}

func (s *Service) markPayoutCompleted(ctx context.Context, store Store, found payment.Payout) error {
	transferID := s.ids.NewID()
	changed, err := found.MarkCompleted(transferID, s.clock.Now())
	if err != nil || !changed {
		return nil
	}
	return s.settlePayout(ctx, store, payoutSettlement{payout: found, eventType: event.TypePayoutCompleted, plan: posting{
		id:    transferID,
		build: transfer.NewPayoutCompletion,
		movement: transfer.Movement{
			OwnerID: found.OwnerID(),
			Key:     transfer.IdempotencyKey("payout-completion-" + string(found.ID())),
			From:    holdAccountFor(found.Amount().Currency()),
			To:      clearingAccountFor(found.Amount().Currency()),
			Amount:  found.Amount(),
		},
	}})
}

func (s *Service) markPayoutFailed(ctx context.Context, store Store, found payment.Payout) error {
	transferID := s.ids.NewID()
	changed, err := found.MarkFailed(transferID, s.clock.Now())
	if err != nil || !changed {
		return nil
	}
	return s.settlePayout(ctx, store, payoutSettlement{payout: found, eventType: event.TypePayoutFailed, plan: posting{
		id:    transferID,
		build: transfer.NewPayoutFailure,
		movement: transfer.Movement{
			OwnerID: found.OwnerID(),
			Key:     transfer.IdempotencyKey("payout-failure-" + string(found.ID())),
			From:    holdAccountFor(found.Amount().Currency()),
			To:      found.AccountID(),
			Amount:  found.Amount(),
		},
	}})
}

func (s *Service) settlePayout(ctx context.Context, store Store, settlement payoutSettlement) error {
	if err := s.postJournal(ctx, store, settlement.plan); err != nil {
		return err
	}
	if err := store.Payouts.Update(ctx, settlement.payout); err != nil {
		return fmt.Errorf("updating payout: %w", err)
	}
	metadata := event.Metadata{ID: event.ID(s.ids.NewID()), At: s.clock.Now()}
	if err := store.Outbox.Add(ctx, event.NewPayoutEvent(metadata, settlement.eventType, settlement.payout)); err != nil {
		return fmt.Errorf("adding event: %w", err)
	}
	return nil
}

func toPayoutOutput(found payment.Payout) PayoutOutput {
	return PayoutOutput{
		ID:          string(found.ID()),
		AccountID:   string(found.AccountID()),
		Status:      string(found.Status()),
		PixKeyType:  string(found.PixKey().Kind()),
		AmountMinor: found.Amount().Minor(),
		Currency:    string(found.Amount().Currency()),
		CreatedAt:   found.Timeline().CreatedAt,
	}
}
