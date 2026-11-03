package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/payment"
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
		changed, err := s.syncCharge(ctx, charge)
		result.Changed += countOf(changed)
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
	olderThan := s.clock.Now().Add(-syncMinimumAge)
	var charges []payment.Charge
	var payouts []payment.Payout
	err := s.uow.Run(ctx, func(ctx context.Context, store Store) error {
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

func (s *Service) syncCharge(ctx context.Context, charge payment.Charge) (bool, error) {
	kind, err := s.chargeStatus(ctx, charge)
	if err != nil {
		return false, fmt.Errorf("checking charge %s: %w", charge.ID(), err)
	}
	if kind != payment.EventIgnored {
		return s.applyPolled(ctx, charge.Reference().ProviderID, kind)
	}
	if !s.clock.Now().After(charge.Pix().ExpiresAt.Add(chargeExpiryGrace)) {
		return false, nil
	}
	return true, s.expireStaleCharge(ctx, charge.ID())
}

func (s *Service) chargeStatus(ctx context.Context, charge payment.Charge) (payment.EventKind, error) {
	if charge.Reference().ProviderID == "" {
		return payment.EventIgnored, nil
	}
	return s.provider.GetCharge(ctx, charge.Reference().ProviderID)
}

func (s *Service) expireStaleCharge(ctx context.Context, id payment.ChargeID) error {
	err := s.uow.Run(ctx, func(ctx context.Context, store Store) error {
		charge, err := store.Charges.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if err := charge.MarkExpired(s.clock.Now()); err != nil {
			return ignoreStale(err)
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
	kind, err := s.provider.GetPayout(ctx, providerID)
	if err != nil {
		return SyncResult{}, fmt.Errorf("checking payout %s: %w", pending.ID(), err)
	}
	if kind == payment.EventIgnored {
		return SyncResult{}, nil
	}
	changed, err := s.applyPolled(ctx, providerID, kind)
	return SyncResult{Changed: countOf(changed)}, err
}

func (s *Service) applyPolled(ctx context.Context, providerID string, kind payment.EventKind) (bool, error) {
	err := s.applyProviderEvent(ctx, payment.ProviderEvent{
		Provider:   s.provider.Name(),
		EventID:    fmt.Sprintf("sync:%s:%s", providerID, kind),
		Kind:       kind,
		ProviderID: providerID,
	})
	return err == nil, err
}

func countOf(changed bool) int {
	if changed {
		return 1
	}
	return 0
}
