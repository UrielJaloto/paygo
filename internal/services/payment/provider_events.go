package payment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

type movementBuilder func(id transfer.ID, movement transfer.Movement, createdAt time.Time) (transfer.Transfer, error)

type posting struct {
	id       string
	build    movementBuilder
	movement transfer.Movement
}

func (s *Service) HandleWebhook(ctx context.Context, input WebhookInput) error {
	if input.Provider != s.provider.Name() {
		return fmt.Errorf("%w: %s", payment.ErrUnsupportedProvider, input.Provider)
	}
	parsed, err := s.provider.ParseWebhook(ctx, input.Header, input.Body)
	if err != nil {
		return fmt.Errorf("parsing webhook: %w", err)
	}
	return s.applyProviderEvent(ctx, parsed)
}

func (s *Service) applyProviderEvent(ctx context.Context, occurred payment.ProviderEvent) error {
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		recorded, err := store.Events.Record(ctx, occurred.Provider, occurred.EventID)
		if err != nil {
			return fmt.Errorf("recording event: %w", err)
		}
		if !recorded {
			return nil
		}
		return s.applyEvent(ctx, store, occurred)
	})
	if err != nil {
		return fmt.Errorf("applying provider event %s: %w", occurred.EventID, err)
	}
	return nil
}

func (s *Service) applyEvent(ctx context.Context, store Store, occurred payment.ProviderEvent) error {
	switch occurred.Kind {
	case payment.EventChargePaid:
		return s.markChargePaid(ctx, store, occurred.ProviderID)
	case payment.EventChargeExpired:
		return s.markChargeExpired(ctx, store, occurred.ProviderID)
	case payment.EventPayoutDone, payment.EventPayoutFailed:
		return s.applyPayoutEvent(ctx, store, occurred)
	}
	return nil
}

func (s *Service) markChargePaid(ctx context.Context, store Store, providerID string) error {
	charge, err := store.Charges.FindByProviderID(ctx, providerID)
	if err != nil {
		return fmt.Errorf("finding charge: %w", err)
	}
	transferID := s.ids.NewID()
	changed, err := charge.MarkPaid(transferID, s.clock.Now())
	if err != nil || !changed {
		return ignoreStale(err)
	}
	err = s.postJournal(ctx, store, posting{id: transferID, build: transfer.NewCharge, movement: transfer.Movement{
		OwnerID: charge.OwnerID(),
		Key:     transfer.IdempotencyKey("charge-" + string(charge.ID())),
		From:    clearingAccountFor(charge.Amount().Currency()),
		To:      charge.AccountID(),
		Amount:  charge.Amount(),
	}})
	if err != nil {
		return err
	}
	return s.publishCharge(ctx, store, charge, event.TypeChargePaid)
}

func (s *Service) markChargeExpired(ctx context.Context, store Store, providerID string) error {
	charge, err := store.Charges.FindByProviderID(ctx, providerID)
	if err != nil {
		return fmt.Errorf("finding charge: %w", err)
	}
	if err := charge.MarkExpired(s.clock.Now()); err != nil {
		return ignoreStale(err)
	}
	return s.publishCharge(ctx, store, charge, event.TypeChargeExpired)
}

func ignoreStale(err error) error {
	if errors.Is(err, payment.ErrInvalidTransition) {
		return nil
	}
	return err
}

func (s *Service) publishCharge(ctx context.Context, store Store, charge payment.Charge, eventType event.Type) error {
	if err := store.Charges.Update(ctx, charge); err != nil {
		return fmt.Errorf("updating charge: %w", err)
	}
	metadata := event.Metadata{ID: event.ID(s.ids.NewID()), At: s.clock.Now()}
	if err := store.Outbox.Add(ctx, event.NewChargeEvent(metadata, eventType, charge)); err != nil {
		return fmt.Errorf("adding event: %w", err)
	}
	return nil
}

func (s *Service) postJournal(ctx context.Context, store Store, plan posting) error {
	ids := []account.ID{plan.movement.From, plan.movement.To}
	slices.Sort(ids)
	if _, err := store.Accounts.LockByIDs(ctx, ids); err != nil {
		return fmt.Errorf("locking accounts: %w", err)
	}
	return s.recordPosting(ctx, store, plan)
}

func (s *Service) recordPosting(ctx context.Context, store Store, plan posting) error {
	created, err := plan.build(transfer.ID(plan.id), plan.movement, s.clock.Now())
	if err != nil {
		return err
	}
	journal, err := created.BuildJournal([2]string{s.ids.NewID(), s.ids.NewID()})
	if err != nil {
		return err
	}
	if err := store.Ledger.Append(ctx, journal); err != nil {
		return fmt.Errorf("appending journal: %w", err)
	}
	fingerprint := transfer.Request{Kind: created.Kind(), From: created.From(), To: created.To(), Amount: created.Amount()}.CalculateFingerprint()
	if err := store.Transfers.Save(ctx, created, fingerprint); err != nil {
		return fmt.Errorf("saving transfer: %w", err)
	}
	return nil
}
