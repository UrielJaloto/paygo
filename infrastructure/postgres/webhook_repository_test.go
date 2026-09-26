//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/services/dispatch"
	webhookservice "github.com/UrielJaloto/paygo/services/webhook"
)

const (
	testHash  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testLease = 30 * time.Second
)

var testSecret = "whsec_" + strings.Repeat("a", 43)

type webhookFixture struct {
	transferFixture
	endpoints  *postgres.EndpointRepository
	deliveries *postgres.DeliveryRepository
	outbox     *postgres.OutboxRepository
}

func newWebhookFixture(t *testing.T) webhookFixture {
	t.Helper()
	base := newTransferFixture(t)
	return webhookFixture{
		transferFixture: base,
		endpoints:       postgres.NewEndpointRepository(base.pool),
		deliveries:      postgres.NewDeliveryRepository(base.pool),
		outbox:          postgres.NewOutboxRepository(base.pool),
	}
}

func (f webhookFixture) saveEndpoint(t *testing.T, sequence int, owner apikey.OwnerID) webhook.Endpoint {
	t.Helper()
	endpoint, err := webhook.NewEndpoint(webhook.EndpointID(testID(sequence)), owner, "https://example.com/hook", testSecret, testTime)
	if err != nil {
		t.Fatalf("NewEndpoint() error = %v", err)
	}
	if err := f.endpoints.Save(context.Background(), endpoint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return endpoint
}

func (f webhookFixture) addEvent(t *testing.T, sequence int) event.ID {
	t.Helper()
	saved := f.save(t, sequence, transfer.IdempotencyKey("key-"+testID(sequence)))
	id := event.ID(testID(sequence + 1))
	metadata := event.Metadata{ID: id, At: testTime}
	if err := f.outbox.Add(context.Background(), event.NewTransferEvent(metadata, saved, []apikey.OwnerID{testOwner})); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	return id
}

func (f webhookFixture) saveDeliveries(t *testing.T, base int, due time.Time, count int) []webhook.DeliveryID {
	t.Helper()
	endpoint := f.saveEndpoint(t, base, testOwner)
	eventID := f.addEvent(t, base+1)
	deliveries := make([]webhook.Delivery, 0, count)
	ids := make([]webhook.DeliveryID, 0, count)
	for offset := range count {
		id := webhook.DeliveryID(testID(base + 10 + offset))
		deliveries = append(deliveries, webhook.NewDelivery(id, eventID, endpoint.ID(), testOwner, due))
		ids = append(ids, id)
	}
	if err := f.deliveries.SaveAll(context.Background(), deliveries); err != nil {
		t.Fatalf("SaveAll() error = %v", err)
	}
	return ids
}

func TestAPIKey_RoundTrip_FindsAndRevokes(t *testing.T) {
	repository := postgres.NewAPIKeyRepository(newTestPool(t))
	ctx := context.Background()
	key, err := apikey.NewMoney(apikey.ID(testID(1)), testOwner, "abcd1234", testHash, testTime)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	if err := repository.Save(ctx, key); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	found, err := repository.FindByPrefix(ctx, "abcd1234")
	if err != nil {
		t.Fatalf("FindByPrefix() error = %v", err)
	}
	assertChecks(t, map[string]bool{
		"id":         found.ID() == key.ID(),
		"owner":      found.OwnerID() == testOwner,
		"hash":       found.Hash() == testHash,
		"active":     found.IsActive(),
		"created at": found.CreatedAt().Equal(testTime),
	})
	if err := found.Revoke(testTime); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if err := repository.Update(ctx, found); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	revoked, err := repository.FindByID(ctx, key.ID())
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if !revoked.RevokedAt().Equal(testTime) {
		t.Fatalf("revoked at = %v, want %v", revoked.RevokedAt(), testTime)
	}
}

func TestAPIKeyFind_Missing_ReturnsNotFound(t *testing.T) {
	repository := postgres.NewAPIKeyRepository(newTestPool(t))
	if _, err := repository.FindByID(context.Background(), apikey.ID(testID(1))); !errors.Is(err, apikey.ErrAPIKeyNotFound) {
		t.Fatalf("FindByID() error = %v, want %v", err, apikey.ErrAPIKeyNotFound)
	}
}

func TestEndpoints_ActiveAndInactive_CountAndListOnlyActive(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	fixture.saveEndpoint(t, 100, testOwner)
	second := fixture.saveEndpoint(t, 101, testOwner)
	disabled := fixture.saveEndpoint(t, 102, testOwner)
	fixture.saveEndpoint(t, 103, otherOwner)
	if err := disabled.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if err := fixture.endpoints.Update(ctx, disabled); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	active, err := fixture.endpoints.CountActiveByOwner(ctx, testOwner)
	if err != nil || active != 2 {
		t.Fatalf("CountActiveByOwner() = %d, %v, want 2", active, err)
	}
	listed, err := fixture.endpoints.ListActiveByOwners(ctx, []apikey.OwnerID{testOwner})
	if err != nil || len(listed) != 2 || listed[1].ID() != second.ID() {
		t.Fatalf("ListActiveByOwners() = %+v, %v", listed, err)
	}
	all, err := fixture.endpoints.ListByOwner(ctx, testOwner)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListByOwner() = %+v, %v", all, err)
	}
}

func TestEndpointFindByID_SavedAndMissing_ReturnsEndpointOrNotFound(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	foreign := fixture.saveEndpoint(t, 103, otherOwner)
	found, err := fixture.endpoints.FindByID(ctx, foreign.ID())
	if err != nil || found.OwnerID() != otherOwner || found.Secret() != testSecret || !found.IsActive() {
		t.Fatalf("FindByID() = %+v, %v", found, err)
	}
	if _, err := fixture.endpoints.FindByID(ctx, webhook.EndpointID(testID(9))); !errors.Is(err, webhook.ErrEndpointNotFound) {
		t.Fatalf("FindByID() error = %v, want %v", err, webhook.ErrEndpointNotFound)
	}
}

func TestOutboxClaimUnpublished_HeldRows_AreSkipped(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	for _, sequence := range []int{100, 110, 120} {
		fixture.addEvent(t, sequence)
	}
	unitOfWork := postgres.NewDispatchUnitOfWork(fixture.pool, &sequentialIDs{})
	err := unitOfWork.Run(ctx, func(ctx context.Context, held dispatch.Store) error {
		first, err := held.Outbox.ClaimUnpublished(ctx, 2)
		if err != nil || len(first) != 2 {
			return fmt.Errorf("first claim = %d, %w", len(first), err)
		}
		if first[0].Transfer().ID() == "" || first[0].Type() != event.TypeTransferCompleted {
			return fmt.Errorf("claimed event = %+v", first[0])
		}
		second, err := fixture.outbox.ClaimUnpublished(ctx, 2)
		if err != nil || len(second) != 1 {
			return fmt.Errorf("second claim = %d, %w", len(second), err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestOutboxMarkPublished_Event_IsNotClaimedAgain(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	id := fixture.addEvent(t, 100)
	if err := fixture.outbox.MarkPublished(ctx, []event.ID{id}); err != nil {
		t.Fatalf("MarkPublished() error = %v", err)
	}
	claimed, err := fixture.outbox.ClaimUnpublished(ctx, 10)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("ClaimUnpublished() = %+v, %v, want none", claimed, err)
	}
}

func TestClaimDue_DueAndFuture_ClaimsDueAndMovesLease(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	due := fixture.saveDeliveries(t, 100, testTime, 1)
	fixture.saveDeliveries(t, 200, testTime.Add(time.Hour), 1)
	claims, err := fixture.deliveries.ClaimDue(ctx, testTime, testLease, 10)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claims) != 1 || claims[0].Delivery.ID() != due[0] {
		t.Fatalf("claims = %+v, want %v", claims, due)
	}
	claim := claims[0]
	if want := testTime.Add(testLease); !claim.Delivery.NextAttemptAt().Equal(want) {
		t.Fatalf("next attempt = %v, want %v", claim.Delivery.NextAttemptAt(), want)
	}
	if claim.Endpoint.Secret() != testSecret || claim.Event.Transfer().ID() == "" || !slices.Equal(claim.Event.OwnerIDs(), []apikey.OwnerID{testOwner}) {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestClaimDue_Concurrent_NeverReturnsSameDelivery(t *testing.T) {
	fixture := newWebhookFixture(t)
	fixture.saveDeliveries(t, 100, testTime, 20)
	results := make([][]dispatch.Claim, 2)
	failures := make([]error, 2)
	var claimers sync.WaitGroup
	for position := range results {
		claimers.Go(func() {
			results[position], failures[position] = fixture.deliveries.ClaimDue(context.Background(), testTime, testLease, 10)
		})
	}
	claimers.Wait()
	if err := errors.Join(failures...); err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	seen := make(map[webhook.DeliveryID]bool)
	for _, claim := range slices.Concat(results...) {
		if seen[claim.Delivery.ID()] {
			t.Fatalf("delivery %s claimed twice", claim.Delivery.ID())
		}
		seen[claim.Delivery.ID()] = true
	}
}

func TestDeliveries_DeadFilter_PagesAndUpdates(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	ids := fixture.saveDeliveries(t, 100, testTime, 3)
	err := postgres.NewWebhookUnitOfWork(fixture.pool).Run(ctx, func(ctx context.Context, store webhookservice.Store) error {
		return killDeliveries(ctx, store, ids[:2])
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	filter := webhookservice.DeliveryFilter{Owner: testOwner, Status: webhook.DeliveryDead}
	first, err := fixture.deliveries.ListByOwner(ctx, filter, page.Cursor{}, 1)
	if err != nil || len(first) != 1 || first[0].ID() != ids[1] || first[0].LastError() != "gone" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := fixture.deliveries.ListByOwner(ctx, filter, page.NewCursor(string(ids[1])), 10)
	if err != nil || len(second) != 1 || second[0].ID() != ids[0] {
		t.Fatalf("second page = %+v, %v", second, err)
	}
	assertOnlyPending(t, fixture, ids[2])
}

func assertOnlyPending(t *testing.T, fixture webhookFixture, want webhook.DeliveryID) {
	t.Helper()
	pending, err := fixture.deliveries.ListPendingByEndpoint(context.Background(), webhook.EndpointID(testID(100)))
	if err != nil || len(pending) != 1 || pending[0].ID() != want {
		t.Fatalf("pending = %+v, %v, want only %s", pending, err, want)
	}
}

func TestDeliveryFindByID_Missing_ReturnsNotFound(t *testing.T) {
	fixture := newWebhookFixture(t)
	if _, err := fixture.deliveries.FindByID(context.Background(), webhook.DeliveryID(testID(9))); !errors.Is(err, webhook.ErrDeliveryNotFound) {
		t.Fatalf("FindByID() error = %v, want %v", err, webhook.ErrDeliveryNotFound)
	}
}

func TestClaims_TransferLookupFails_ReturnError(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	fixture.saveDeliveries(t, 100, testTime, 1)
	if _, err := fixture.pool.Exec(ctx, "ALTER TABLE transfers RENAME TO transfers_unavailable"); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	_, claimErr := fixture.deliveries.ClaimDue(ctx, testTime, testLease, 1)
	_, outboxErr := fixture.outbox.ClaimUnpublished(ctx, 1)
	for name, err := range map[string]error{"ClaimDue": claimErr, "ClaimUnpublished": outboxErr} {
		if err == nil {
			t.Errorf("%s() error = nil, want an error", name)
		}
	}
}

func killDeliveries(ctx context.Context, store webhookservice.Store, ids []webhook.DeliveryID) error {
	for _, id := range ids {
		found, err := store.Deliveries.FindByID(ctx, id)
		if err != nil {
			return err
		}
		found.MarkDead("gone", testTime)
		if err := store.Deliveries.Update(ctx, found); err != nil {
			return err
		}
	}
	return nil
}

func TestFanOut_RealDatabase_CreatesOneDeliveryPerEndpoint(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	fixture.saveEndpoint(t, 100, testOwner)
	fixture.saveEndpoint(t, 101, testOwner)
	fixture.addEvent(t, 110)
	service := dispatch.NewMoney(postgres.NewDispatchUnitOfWork(fixture.pool, &sequentialIDs{}), fixture.deliveries, nil, fixedClock{}, nil)
	result, err := service.FanOut(ctx, 10)
	if err != nil {
		t.Fatalf("FanOut() error = %v", err)
	}
	if result.Events != 1 || result.Deliveries != 2 {
		t.Fatalf("result = %+v, want 1 event and 2 deliveries", result)
	}
	if rows := countRows(t, fixture.pool, "webhook_deliveries"); rows != 2 {
		t.Fatalf("delivery rows = %d, want 2", rows)
	}
}

func TestWebhookRepositories_ClosedPool_ReturnErrors(t *testing.T) {
	fixture := newWebhookFixture(t)
	ctx := context.Background()
	keys := postgres.NewAPIKeyRepository(fixture.pool)
	fixture.pool.Close()
	_, claimErr := fixture.deliveries.ClaimDue(ctx, testTime, testLease, 1)
	_, outboxErr := fixture.outbox.ClaimUnpublished(ctx, 1)
	_, keyErr := keys.FindByPrefix(ctx, "abcd1234")
	for name, err := range map[string]error{"ClaimDue": claimErr, "ClaimUnpublished": outboxErr, "FindByPrefix": keyErr} {
		if err == nil {
			t.Errorf("%s() error = nil, want an error", name)
		}
	}
}
