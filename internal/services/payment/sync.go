package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
)

const (
	syncMinimumAge    = time.Minute
	chargeExpiryGrace = time.Hour
)

func (s *Service) Sync(ctx context.Context, limit int) (SyncResult, error) {
	charges, payouts, err := s.listPending(ctx, limit)
	if err != nil {
		return SyncResult{}, fmt.Errorf("listing pending payments: %w", err)
	}
	result := SyncResult{Checked: len(charges) + len(payouts)}
	var failures []error
	for _, charge := range charges {
		outcome, err := s.syncCharge(ctx, charge)
		result.Changed += outcome.Changed
		failures = append(failures, err)
	}
	for _, pending := range payouts {
		outcome, err := s.syncPayout(ctx, pending)
		result.Changed += outcome.Changed
		result.Retried += outcome.Retried
		failures = append(failures, err)
	}
	return result, errors.Join(failures...)
}

func (s *Service) listPending(ctx context.Context, limit int) ([]payment.Charge, []payment.Payout, error) {
	olderThan := s.now().Add(-syncMinimumAge)
	var charges []payment.Charge
	var payouts []payment.Payout
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		var err error
		charges, err = store.Charges.ListPending(ctx, olderThan, limit)
		if err != nil {
			return err
		}
		payouts, err = store.Payouts.ListPending(ctx, olderThan, limit)
		return err
	})
	return charges, payouts, err
}

func (s *Service) syncCharge(ctx context.Context, charge payment.Charge) (SyncResult, error) {
	kind, err := s.fetchChargeStatus(ctx, charge)
	if err != nil {
		return SyncResult{}, fmt.Errorf("checking charge %s: %w", charge.ID(), err)
	}
	if kind != payment.EventIgnored {
		return s.applyPolledStatus(ctx, charge.Reference().ProviderID, kind)
	}
	if !s.now().After(charge.Pix().ExpiresAt.Add(chargeExpiryGrace)) {
		return SyncResult{}, nil
	}
	return SyncResult{Changed: 1}, s.expireStaleCharge(ctx, charge.ID())
}

func (s *Service) fetchChargeStatus(ctx context.Context, charge payment.Charge) (string, error) {
	if charge.Reference().ProviderID == "" {
		return payment.EventIgnored, nil
	}
	return s.provider.FetchCharge(ctx, charge.Reference().ProviderID)
}

func (s *Service) expireStaleCharge(ctx context.Context, id string) error {
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		charge, err := store.Charges.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if err := charge.MarkExpired(s.now()); err != nil {
			return ignoreInvalidTransition(err)
		}
		return s.publishCharge(ctx, store, charge, event.TypeChargeExpired)
	})
	if err != nil {
		return fmt.Errorf("expiring charge %s: %w", id, err)
	}
	return nil
}

func (s *Service) syncPayout(ctx context.Context, pending payment.Payout) (SyncResult, error) {
	providerID := pending.Reference().ProviderID
	if providerID == "" {
		_, err := s.sendPayout(ctx, pending)
		return SyncResult{Retried: 1}, err
	}
	kind, err := s.provider.FetchPayout(ctx, providerID)
	if err != nil {
		return SyncResult{}, fmt.Errorf("checking payout %s: %w", pending.ID(), err)
	}
	if kind == payment.EventIgnored {
		return SyncResult{}, nil
	}
	return s.applyPolledStatus(ctx, providerID, kind)
}

func (s *Service) applyPolledStatus(ctx context.Context, providerID string, kind string) (SyncResult, error) {
	err := s.applyProviderEvent(ctx, payment.ProviderEvent{
		Provider:   s.provider.Name(),
		EventID:    fmt.Sprintf("sync:%s:%s", providerID, kind),
		Kind:       kind,
		ProviderID: providerID,
	})
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Changed: 1}, nil
}
