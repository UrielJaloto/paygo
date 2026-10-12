package storetest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/services/dispatch"
	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
)

const contractLease = 30 * time.Second

type EndpointStore interface {
	webhookservice.EndpointRepository
	dispatch.EndpointFinder
}

type DeliveryStore interface {
	webhookservice.DeliveryRepository
	dispatch.DeliveryWriter
	dispatch.DeliveryQueue
}

type OutboxStore interface {
	dispatch.OutboxReader
	Add(ctx context.Context, e event.Event) error
}

type WebhookStores struct {
	Endpoints  EndpointStore
	Deliveries DeliveryStore
	Outbox     OutboxStore
	Unit       webhookservice.UnitOfWork
	Dispatch   dispatch.UnitOfWork
}

func RunWebhookContract(t *testing.T, newStores func(t *testing.T) WebhookStores) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, stores WebhookStores)
	}{
		{"endpoint round trips and misses", endpointRoundTrip},
		{"endpoint listings follow owner and activity", endpointListings},
		{"failed work rolls endpoints back", endpointRollback},
		{"delivery listings filter and page", deliveryListings},
		{"delivery update stores the outcome", deliveryUpdate},
		{"update of missing rows changes nothing", updateMissing},
		{"claimed delivery stays invisible until the lease ends", claimLease},
		{"claim takes the earliest due deliveries", claimOrder},
		{"outbox claims unpublished events in id order", outboxClaim},
		{"outbox skips events claimed by another unit", outboxSkipLocked},
		{"dispatch unit rolls back on failure", dispatchRollback},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.run(t, newStores(t))
		})
	}
}

func newEndpoint(t *testing.T, sequence int, owner apikey.OwnerID) webhook.Endpoint {
	t.Helper()
	secret := "whsec_" + strings.Repeat("a", 43)
	created, err := webhook.NewEndpoint(webhook.EndpointID(ID(sequence)), owner, "https://example.com/hook", secret, At)
	NoError(t, err)
	return created
}

func (s WebhookStores) saveEndpoint(t *testing.T, sequence int) webhook.Endpoint {
	t.Helper()
	saved := newEndpoint(t, sequence, OwnerA)
	NoError(t, s.Endpoints.Save(context.Background(), saved))
	return saved
}

func (s WebhookStores) addEvent(t *testing.T, sequence int) event.ID {
	t.Helper()
	charge, err := payment.NewCharge(payment.ChargeID(ID(sequence+1)), payment.ChargeDraft{
		OwnerID:   OwnerA,
		AccountID: "01900000-0000-7000-8000-000000000001",
		Amount:    money.NewMoney(100, money.BRL),
		Payer:     payment.Payer{Document: "52998224725", Name: "Ana"},
		Provider:  "stub",
	}, At.Add(time.Hour), At)
	NoError(t, err)
	id := event.ID(ID(sequence))
	NoError(t, s.Outbox.Add(context.Background(), event.NewChargeEvent(event.Metadata{ID: id, At: At}, event.TypeChargeExpired, charge)))
	return id
}

func (s WebhookStores) saveDelivery(t *testing.T, sequence int, due time.Time) webhook.Delivery {
	t.Helper()
	endpoint := s.saveEndpoint(t, sequence+1000)
	eventID := s.addEvent(t, sequence+2000)
	saved := webhook.NewDelivery(webhook.DeliveryID(ID(sequence)), eventID, endpoint.ID(), OwnerA, due)
	NoError(t, s.Deliveries.SaveAll(context.Background(), []webhook.Delivery{saved}))
	return saved
}

func endpointIDs(endpoints []webhook.Endpoint) []webhook.EndpointID {
	ids := make([]webhook.EndpointID, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ids = append(ids, endpoint.ID())
	}
	return ids
}

func deliveryIDs(deliveries []webhook.Delivery) []webhook.DeliveryID {
	ids := make([]webhook.DeliveryID, 0, len(deliveries))
	for _, delivery := range deliveries {
		ids = append(ids, delivery.ID())
	}
	return ids
}

func eventIDs(events []event.Event) []event.ID {
	ids := make([]event.ID, 0, len(events))
	for _, occurred := range events {
		ids = append(ids, occurred.ID())
	}
	return ids
}

func endpointRoundTrip(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	saved := stores.saveEndpoint(t, 1)
	found, err := stores.Endpoints.FindByID(ctx, saved.ID())
	NoError(t, err)
	Equal(t, found.OwnerID(), OwnerA)
	Equal(t, found.URL(), saved.URL())
	Equal(t, found.Secret(), saved.Secret())
	Equal(t, found.IsActive(), true)
	_, err = stores.Endpoints.FindByID(ctx, webhook.EndpointID(ID(9)))
	ErrorIs(t, err, webhook.ErrEndpointNotFound)
}

func endpointListings(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	first, second := stores.saveEndpoint(t, 1), stores.saveEndpoint(t, 2)
	other := newEndpoint(t, 3, OwnerB)
	NoError(t, stores.Endpoints.Save(ctx, other))
	NoError(t, second.Disable())
	NoError(t, stores.Endpoints.Update(ctx, second))
	listed, err := stores.Endpoints.ListByOwner(ctx, OwnerA)
	NoError(t, err)
	EqualSlices(t, endpointIDs(listed), []webhook.EndpointID{first.ID(), second.ID()})
	active, err := stores.Endpoints.CountActiveByOwner(ctx, OwnerA)
	NoError(t, err)
	Equal(t, active, 1)
	targets, err := stores.Endpoints.ListActiveByOwners(ctx, []apikey.OwnerID{OwnerA, OwnerB})
	NoError(t, err)
	EqualSlices(t, endpointIDs(targets), []webhook.EndpointID{first.ID(), other.ID()})
}

func endpointRollback(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	err := stores.Unit.Run(ctx, func(ctx context.Context, store webhookservice.Store) error {
		NoError(t, store.Endpoints.Save(ctx, newEndpoint(t, 1, OwnerA)))
		return errWorkFailed
	})
	ErrorIs(t, err, errWorkFailed)
	_, err = stores.Endpoints.FindByID(ctx, webhook.EndpointID(ID(1)))
	ErrorIs(t, err, webhook.ErrEndpointNotFound)
	NoError(t, stores.Unit.Run(ctx, func(ctx context.Context, store webhookservice.Store) error {
		return store.Endpoints.Save(ctx, newEndpoint(t, 1, OwnerA))
	}))
	_, err = stores.Endpoints.FindByID(ctx, webhook.EndpointID(ID(1)))
	NoError(t, err)
}

func deliveryListings(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	first, second := stores.saveDelivery(t, 1, At), stores.saveDelivery(t, 2, At)
	NoError(t, second.MarkSucceeded(At))
	NoError(t, stores.Deliveries.Update(ctx, second))
	all := webhookservice.DeliveryFilter{Owner: OwnerA}
	newest, err := stores.Deliveries.ListByOwner(ctx, all, page.Cursor{}, 1)
	NoError(t, err)
	EqualSlices(t, deliveryIDs(newest), []webhook.DeliveryID{second.ID()})
	older, err := stores.Deliveries.ListByOwner(ctx, all, page.NewCursor(string(second.ID())), 10)
	NoError(t, err)
	EqualSlices(t, deliveryIDs(older), []webhook.DeliveryID{first.ID()})
	pendingOnly := webhookservice.DeliveryFilter{Owner: OwnerA, Status: webhook.DeliveryPending}
	pending, err := stores.Deliveries.ListByOwner(ctx, pendingOnly, page.Cursor{}, 10)
	NoError(t, err)
	EqualSlices(t, deliveryIDs(pending), []webhook.DeliveryID{first.ID()})
	byEndpoint, err := stores.Deliveries.ListPendingByEndpoint(ctx, first.EndpointID())
	NoError(t, err)
	EqualSlices(t, deliveryIDs(byEndpoint), []webhook.DeliveryID{first.ID()})
}

func deliveryUpdate(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	saved := stores.saveDelivery(t, 1, At)
	saved.MarkDead("gone", At)
	NoError(t, stores.Deliveries.Update(ctx, saved))
	found, err := stores.Deliveries.FindByID(ctx, saved.ID())
	NoError(t, err)
	Equal(t, found.Status(), saved.Status())
	Equal(t, found.LastError(), "gone")
	_, err = stores.Deliveries.FindByID(ctx, webhook.DeliveryID(ID(9)))
	ErrorIs(t, err, webhook.ErrDeliveryNotFound)
}

func updateMissing(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	endpoint := newEndpoint(t, 1, OwnerA)
	NoError(t, stores.Endpoints.Update(ctx, endpoint))
	_, err := stores.Endpoints.FindByID(ctx, endpoint.ID())
	ErrorIs(t, err, webhook.ErrEndpointNotFound)
	delivery := webhook.NewDelivery(webhook.DeliveryID(ID(2)), event.ID(ID(3)), endpoint.ID(), OwnerA, At)
	NoError(t, stores.Deliveries.Update(ctx, delivery))
	_, err = stores.Deliveries.FindByID(ctx, delivery.ID())
	ErrorIs(t, err, webhook.ErrDeliveryNotFound)
}

func claimLease(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	saved := stores.saveDelivery(t, 1, At)
	claims, err := stores.Deliveries.ClaimDue(ctx, At, contractLease, 10)
	NoError(t, err)
	Equal(t, len(claims), 1)
	Equal(t, claims[0].Delivery.ID(), saved.ID())
	Equal(t, claims[0].Delivery.NextAttemptAt().Equal(At.Add(contractLease)), true)
	Equal(t, claims[0].Endpoint.ID(), saved.EndpointID())
	Equal(t, claims[0].Event.ID(), saved.EventID())
	hidden, err := stores.Deliveries.ClaimDue(ctx, At.Add(contractLease-time.Second), contractLease, 10)
	NoError(t, err)
	Equal(t, len(hidden), 0)
	again, err := stores.Deliveries.ClaimDue(ctx, At.Add(contractLease), contractLease, 10)
	NoError(t, err)
	Equal(t, len(again), 1)
}

func claimOrder(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	late := stores.saveDelivery(t, 1, At.Add(time.Minute))
	early := stores.saveDelivery(t, 2, At)
	stores.saveDelivery(t, 3, At.Add(time.Hour))
	opening, err := stores.Deliveries.ClaimDue(ctx, At.Add(time.Minute), contractLease, 1)
	NoError(t, err)
	Equal(t, len(opening), 1)
	Equal(t, opening[0].Delivery.ID(), early.ID())
	rest, err := stores.Deliveries.ClaimDue(ctx, At.Add(time.Minute), contractLease, 10)
	NoError(t, err)
	Equal(t, len(rest), 1)
	Equal(t, rest[0].Delivery.ID(), late.ID())
}

func outboxClaim(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	second := stores.addEvent(t, 2)
	first := stores.addEvent(t, 1)
	claimed, err := stores.Outbox.ClaimUnpublished(ctx, 1)
	NoError(t, err)
	EqualSlices(t, eventIDs(claimed), []event.ID{first})
	Equal(t, claimed[0].Type(), event.TypeChargeExpired)
	EqualSlices(t, claimed[0].OwnerIDs(), []apikey.OwnerID{OwnerA})
	NoError(t, stores.Outbox.MarkPublished(ctx, []event.ID{first}))
	rest, err := stores.Outbox.ClaimUnpublished(ctx, 10)
	NoError(t, err)
	EqualSlices(t, eventIDs(rest), []event.ID{second})
}

func outboxSkipLocked(t *testing.T, stores WebhookStores) {
	id := stores.addEvent(t, 1)
	NoError(t, stores.Dispatch.Run(context.Background(), func(ctx context.Context, store dispatch.Store) error {
		claimed, err := store.Outbox.ClaimUnpublished(ctx, 10)
		NoError(t, err)
		EqualSlices(t, eventIDs(claimed), []event.ID{id})
		repeated, err := store.Outbox.ClaimUnpublished(ctx, 10)
		NoError(t, err)
		EqualSlices(t, eventIDs(repeated), []event.ID{id})
		return stores.Dispatch.Run(ctx, func(ctx context.Context, other dispatch.Store) error {
			skipped, err := other.Outbox.ClaimUnpublished(ctx, 10)
			NoError(t, err)
			Equal(t, len(skipped), 0)
			return nil
		})
	}))
}

func dispatchRollback(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	id := stores.addEvent(t, 1)
	err := stores.Dispatch.Run(ctx, func(ctx context.Context, store dispatch.Store) error {
		NoError(t, store.Outbox.MarkPublished(ctx, []event.ID{id}))
		return errWorkFailed
	})
	ErrorIs(t, err, errWorkFailed)
	claimed, err := stores.Outbox.ClaimUnpublished(ctx, 10)
	NoError(t, err)
	EqualSlices(t, eventIDs(claimed), []event.ID{id})
}
