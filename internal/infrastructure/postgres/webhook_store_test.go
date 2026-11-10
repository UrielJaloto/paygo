//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
	"github.com/UrielJaloto/paygo/internal/services/dispatch"
	webhookservice "github.com/UrielJaloto/paygo/internal/services/webhook"
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

func runWebhookContract(t *testing.T, newStores func(t *testing.T) WebhookStores) {
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
	created, err := webhook.NewEndpoint(testID(sequence), owner, "https://example.com/hook", secret, testTime)
	noError(t, err)
	return created
}

func (s WebhookStores) saveEndpoint(t *testing.T, sequence int) webhook.Endpoint {
	t.Helper()
	saved := newEndpoint(t, sequence, testOwner)
	noError(t, s.Endpoints.Save(context.Background(), saved))
	return saved
}

func (s WebhookStores) addEvent(t *testing.T, sequence int) string {
	t.Helper()
	charge, err := payment.NewCharge(testID(sequence+1), payment.ChargeDraft{
		OwnerID:   testOwner,
		AccountID: "01900000-0000-7000-8000-000000000001",
		Amount:    money.NewMoney(100, money.BRL),
		Payer:     payment.Payer{Document: "52998224725", Name: "Ana"},
		Provider:  "stub",
	}, testTime.Add(time.Hour), testTime)
	noError(t, err)
	id := testID(sequence)
	noError(t, s.Outbox.Add(context.Background(), event.NewChargeEvent(event.Metadata{ID: id, At: testTime}, event.TypeChargeExpired, charge)))
	return id
}

func (s WebhookStores) saveDelivery(t *testing.T, sequence int, due time.Time) webhook.Delivery {
	t.Helper()
	endpoint := s.saveEndpoint(t, sequence+1000)
	eventID := s.addEvent(t, sequence+2000)
	saved := webhook.NewDelivery(testID(sequence), eventID, endpoint.ID(), testOwner, due)
	noError(t, s.Deliveries.SaveAll(context.Background(), []webhook.Delivery{saved}))
	return saved
}

func endpointIDs(endpoints []webhook.Endpoint) []string {
	ids := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ids = append(ids, endpoint.ID())
	}
	return ids
}

func deliveryIDs(deliveries []webhook.Delivery) []string {
	ids := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		ids = append(ids, delivery.ID())
	}
	return ids
}

func eventIDs(events []event.Event) []string {
	ids := make([]string, 0, len(events))
	for _, occurred := range events {
		ids = append(ids, occurred.ID())
	}
	return ids
}

func endpointRoundTrip(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	saved := stores.saveEndpoint(t, 1)
	found, err := stores.Endpoints.FindByID(ctx, saved.ID())
	noError(t, err)
	equal(t, found.OwnerID(), testOwner)
	equal(t, found.URL(), saved.URL())
	equal(t, found.Secret(), saved.Secret())
	equal(t, found.IsActive(), true)
	_, err = stores.Endpoints.FindByID(ctx, testID(9))
	errorIs(t, err, webhook.ErrEndpointNotFound)
}

func endpointListings(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	first, second := stores.saveEndpoint(t, 1), stores.saveEndpoint(t, 2)
	other := newEndpoint(t, 3, otherOwner)
	noError(t, stores.Endpoints.Save(ctx, other))
	noError(t, second.Disable())
	noError(t, stores.Endpoints.Update(ctx, second))
	listed, err := stores.Endpoints.ListByOwner(ctx, testOwner)
	noError(t, err)
	equalSlices(t, endpointIDs(listed), []string{first.ID(), second.ID()})
	active, err := stores.Endpoints.CountActiveByOwner(ctx, testOwner)
	noError(t, err)
	equal(t, active, 1)
	targets, err := stores.Endpoints.ListActiveByOwners(ctx, []apikey.OwnerID{testOwner, otherOwner})
	noError(t, err)
	equalSlices(t, endpointIDs(targets), []string{first.ID(), other.ID()})
}

func endpointRollback(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	err := stores.Unit.Run(ctx, func(ctx context.Context, store webhookservice.Store) error {
		noError(t, store.Endpoints.Save(ctx, newEndpoint(t, 1, testOwner)))
		return errWorkFailed
	})
	errorIs(t, err, errWorkFailed)
	_, err = stores.Endpoints.FindByID(ctx, testID(1))
	errorIs(t, err, webhook.ErrEndpointNotFound)
	noError(t, stores.Unit.Run(ctx, func(ctx context.Context, store webhookservice.Store) error {
		return store.Endpoints.Save(ctx, newEndpoint(t, 1, testOwner))
	}))
	_, err = stores.Endpoints.FindByID(ctx, testID(1))
	noError(t, err)
}

func deliveryListings(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	first, second := stores.saveDelivery(t, 1, testTime), stores.saveDelivery(t, 2, testTime)
	noError(t, second.MarkSucceeded(testTime))
	noError(t, stores.Deliveries.Update(ctx, second))
	all := webhookservice.DeliveryFilter{Owner: testOwner}
	newest, err := stores.Deliveries.ListByOwner(ctx, all, page.Cursor{}, 1)
	noError(t, err)
	equalSlices(t, deliveryIDs(newest), []string{second.ID()})
	older, err := stores.Deliveries.ListByOwner(ctx, all, page.NewCursor(string(second.ID())), 10)
	noError(t, err)
	equalSlices(t, deliveryIDs(older), []string{first.ID()})
	pendingOnly := webhookservice.DeliveryFilter{Owner: testOwner, Status: webhook.DeliveryPending}
	pending, err := stores.Deliveries.ListByOwner(ctx, pendingOnly, page.Cursor{}, 10)
	noError(t, err)
	equalSlices(t, deliveryIDs(pending), []string{first.ID()})
	byEndpoint, err := stores.Deliveries.ListPendingByEndpoint(ctx, first.EndpointID())
	noError(t, err)
	equalSlices(t, deliveryIDs(byEndpoint), []string{first.ID()})
}

func deliveryUpdate(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	saved := stores.saveDelivery(t, 1, testTime)
	saved.MarkDead("gone", testTime)
	noError(t, stores.Deliveries.Update(ctx, saved))
	found, err := stores.Deliveries.FindByID(ctx, saved.ID())
	noError(t, err)
	equal(t, found.Status(), saved.Status())
	equal(t, found.LastError(), "gone")
	_, err = stores.Deliveries.FindByID(ctx, testID(9))
	errorIs(t, err, webhook.ErrDeliveryNotFound)
}

func updateMissing(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	endpoint := newEndpoint(t, 1, testOwner)
	noError(t, stores.Endpoints.Update(ctx, endpoint))
	_, err := stores.Endpoints.FindByID(ctx, endpoint.ID())
	errorIs(t, err, webhook.ErrEndpointNotFound)
	delivery := webhook.NewDelivery(testID(2), testID(3), endpoint.ID(), testOwner, testTime)
	noError(t, stores.Deliveries.Update(ctx, delivery))
	_, err = stores.Deliveries.FindByID(ctx, delivery.ID())
	errorIs(t, err, webhook.ErrDeliveryNotFound)
}

func claimLease(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	saved := stores.saveDelivery(t, 1, testTime)
	claims, err := stores.Deliveries.ClaimDue(ctx, testTime, contractLease, 10)
	noError(t, err)
	equal(t, len(claims), 1)
	equal(t, claims[0].Delivery.ID(), saved.ID())
	equal(t, claims[0].Delivery.NextAttemptAt().Equal(testTime.Add(contractLease)), true)
	equal(t, claims[0].Endpoint.ID(), saved.EndpointID())
	equal(t, claims[0].Event.ID(), saved.EventID())
	hidden, err := stores.Deliveries.ClaimDue(ctx, testTime.Add(contractLease-time.Second), contractLease, 10)
	noError(t, err)
	equal(t, len(hidden), 0)
	again, err := stores.Deliveries.ClaimDue(ctx, testTime.Add(contractLease), contractLease, 10)
	noError(t, err)
	equal(t, len(again), 1)
}

func claimOrder(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	late := stores.saveDelivery(t, 1, testTime.Add(time.Minute))
	early := stores.saveDelivery(t, 2, testTime)
	stores.saveDelivery(t, 3, testTime.Add(time.Hour))
	opening, err := stores.Deliveries.ClaimDue(ctx, testTime.Add(time.Minute), contractLease, 1)
	noError(t, err)
	equal(t, len(opening), 1)
	equal(t, opening[0].Delivery.ID(), early.ID())
	rest, err := stores.Deliveries.ClaimDue(ctx, testTime.Add(time.Minute), contractLease, 10)
	noError(t, err)
	equal(t, len(rest), 1)
	equal(t, rest[0].Delivery.ID(), late.ID())
}

func outboxClaim(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	second := stores.addEvent(t, 2)
	first := stores.addEvent(t, 1)
	claimed, err := stores.Outbox.ClaimUnpublished(ctx, 1)
	noError(t, err)
	equalSlices(t, eventIDs(claimed), []string{first})
	equal(t, claimed[0].Type(), event.TypeChargeExpired)
	equalSlices(t, claimed[0].OwnerIDs(), []apikey.OwnerID{testOwner})
	noError(t, stores.Outbox.MarkPublished(ctx, []string{first}))
	rest, err := stores.Outbox.ClaimUnpublished(ctx, 10)
	noError(t, err)
	equalSlices(t, eventIDs(rest), []string{second})
}

func outboxSkipLocked(t *testing.T, stores WebhookStores) {
	id := stores.addEvent(t, 1)
	noError(t, stores.Dispatch.Run(context.Background(), func(ctx context.Context, store dispatch.Store) error {
		claimed, err := store.Outbox.ClaimUnpublished(ctx, 10)
		noError(t, err)
		equalSlices(t, eventIDs(claimed), []string{id})
		repeated, err := store.Outbox.ClaimUnpublished(ctx, 10)
		noError(t, err)
		equalSlices(t, eventIDs(repeated), []string{id})
		return stores.Dispatch.Run(ctx, func(ctx context.Context, other dispatch.Store) error {
			skipped, err := other.Outbox.ClaimUnpublished(ctx, 10)
			noError(t, err)
			equal(t, len(skipped), 0)
			return nil
		})
	}))
}

func dispatchRollback(t *testing.T, stores WebhookStores) {
	ctx := context.Background()
	id := stores.addEvent(t, 1)
	err := stores.Dispatch.Run(ctx, func(ctx context.Context, store dispatch.Store) error {
		noError(t, store.Outbox.MarkPublished(ctx, []string{id}))
		return errWorkFailed
	})
	errorIs(t, err, errWorkFailed)
	claimed, err := stores.Outbox.ClaimUnpublished(ctx, 10)
	noError(t, err)
	equalSlices(t, eventIDs(claimed), []string{id})
}
