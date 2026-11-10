package payment

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

type payoutSettlement struct {
	payout    payment.Payout
	plan      posting
	eventType event.Type
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
			From:    findHoldAccountID(found.Amount().Currency()),
			To:      findClearingAccountID(found.Amount().Currency()),
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
			From:    findHoldAccountID(found.Amount().Currency()),
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
