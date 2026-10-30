package memory

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/services/dispatch"
)

func (r *DeliveryRepository) ClaimDue(_ context.Context, now time.Time, lease time.Duration, limit int) ([]dispatch.Claim, error) {
	r.store.data.Lock()
	defer r.store.data.Unlock()
	due := r.store.dueDeliveries(now)
	due = due[:min(len(due), limit)]
	claims := make([]dispatch.Claim, 0, len(due))
	for _, delivery := range due {
		leased := leaseDelivery(delivery, now.Add(lease))
		r.store.webhooks.deliveries[leased.ID()] = leased
		claims = append(claims, dispatch.Claim{
			Delivery: leased,
			Endpoint: r.store.webhooks.endpoints[leased.EndpointID()],
			Event:    r.store.currentEvent(r.store.eventByID(leased.EventID())),
		})
	}
	return claims, nil
}

func (s *Store) dueDeliveries(now time.Time) []webhook.Delivery {
	due := make([]webhook.Delivery, 0, len(s.webhooks.deliveries))
	for _, delivery := range s.webhooks.deliveries {
		if delivery.IsDue(now) {
			due = append(due, delivery)
		}
	}
	slices.SortFunc(due, func(a, b webhook.Delivery) int {
		return cmp.Or(a.NextAttemptAt().Compare(b.NextAttemptAt()), cmp.Compare(a.ID(), b.ID()))
	})
	return due
}

func (s *Store) eventByID(id event.ID) event.Event {
	index := slices.IndexFunc(s.events, func(record *outboxRecord) bool {
		return record.event.ID() == id
	})
	if index < 0 {
		return event.Event{}
	}
	return s.events[index].event
}

func leaseDelivery(delivery webhook.Delivery, until time.Time) webhook.Delivery {
	return webhook.RestoreDelivery(webhook.DeliverySnapshot{
		ID:            delivery.ID(),
		EventID:       delivery.EventID(),
		EndpointID:    delivery.EndpointID(),
		OwnerID:       delivery.OwnerID(),
		Status:        delivery.Status(),
		Attempts:      delivery.Attempts(),
		NextAttemptAt: until,
		LastError:     delivery.LastError(),
		UpdatedAt:     delivery.UpdatedAt(),
	})
}
