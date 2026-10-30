package memory_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/memory"
	"github.com/UrielJaloto/paygo/internal/storetest"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

func newWebhookStores(*testing.T) storetest.WebhookStores {
	store := memory.NewStore(storetest.FixedClock{})
	return storetest.WebhookStores{
		Endpoints:  memory.NewEndpointRepository(store),
		Deliveries: memory.NewDeliveryRepository(store),
		Outbox:     memory.NewOutboxRepository(store),
		Unit:       memory.NewWebhookUnitOfWork(store),
		Dispatch:   memory.NewDispatchUnitOfWork(store, &storetest.SequentialIDs{}),
	}
}

func newPaymentStores(*testing.T) storetest.PaymentStores {
	store := memory.NewStore(storetest.FixedClock{})
	return storetest.PaymentStores{
		Base:    newStores(store),
		Charges: memory.NewChargeRepository(store),
		Payouts: memory.NewPayoutRepository(store),
		Events:  memory.NewProviderEventLog(store),
		Unit:    memory.NewPaymentUnitOfWork(store),
		Checker: memory.NewLedgerRepository(store),
	}
}

func TestAPIKeyContract_MemoryStore_Passes(t *testing.T) {
	storetest.RunAPIKeyContract(t, func(*testing.T) apikeyservice.Repository {
		return memory.NewAPIKeyRepository(memory.NewStore(storetest.FixedClock{}))
	})
}

func TestWebhookContract_MemoryStore_Passes(t *testing.T) {
	storetest.RunWebhookContract(t, newWebhookStores)
}

func TestPaymentContract_MemoryStore_Passes(t *testing.T) {
	storetest.RunPaymentContract(t, newPaymentStores)
}

func TestAPIKeySave_DuplicateID_ReturnsDuplicate(t *testing.T) {
	repository := memory.NewAPIKeyRepository(memory.NewStore(storetest.FixedClock{}))
	key, err := apikey.New(apikey.ID(storetest.ID(1)), storetest.OwnerA, "abcdefgh", strings.Repeat("b", 64), storetest.At)
	storetest.NoError(t, err)
	storetest.NoError(t, repository.Save(context.Background(), key))
	storetest.ErrorIs(t, repository.Save(context.Background(), key), memory.ErrDuplicateID)
}

func TestEndpointSave_DuplicateID_ReturnsDuplicate(t *testing.T) {
	stores := newWebhookStores(t)
	endpoint, err := webhook.NewEndpoint(webhook.EndpointID(storetest.ID(1)), storetest.OwnerA, "https://example.com/hook", "whsec_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", storetest.At)
	storetest.NoError(t, err)
	storetest.NoError(t, stores.Endpoints.Save(context.Background(), endpoint))
	storetest.ErrorIs(t, stores.Endpoints.Save(context.Background(), endpoint), memory.ErrDuplicateID)
}

func TestDeliverySaveAll_DuplicateID_ReturnsDuplicate(t *testing.T) {
	stores := newWebhookStores(t)
	delivery := webhook.NewDelivery(webhook.DeliveryID(storetest.ID(1)), "event", "endpoint", storetest.OwnerA, storetest.At)
	storetest.NoError(t, stores.Deliveries.SaveAll(context.Background(), []webhook.Delivery{delivery}))
	storetest.ErrorIs(t, stores.Deliveries.SaveAll(context.Background(), []webhook.Delivery{delivery}), memory.ErrDuplicateID)
}

func TestClaimDue_EventMissing_ReturnsEmptyEvent(t *testing.T) {
	stores := newWebhookStores(t)
	delivery := webhook.NewDelivery(webhook.DeliveryID(storetest.ID(1)), "event", "endpoint", storetest.OwnerA, storetest.At)
	storetest.NoError(t, stores.Deliveries.SaveAll(context.Background(), []webhook.Delivery{delivery}))
	claims, err := stores.Deliveries.ClaimDue(context.Background(), storetest.At, time.Second, 1)
	storetest.NoError(t, err)
	storetest.Equal(t, claims[0].Event.ID(), "")
}

func TestChargeSave_DuplicateID_ReturnsDuplicate(t *testing.T) {
	stores := newPaymentStores(t)
	charge, err := payment.NewCharge(payment.ChargeID(storetest.ID(1)), payment.ChargeDraft{
		OwnerID:   storetest.OwnerA,
		AccountID: storetest.SettlementBRL,
		Amount:    money.New(1000, money.BRL),
		Payer:     payment.Payer{Document: "52998224725", Name: "Ana"},
		Provider:  "stub",
	}, storetest.At.Add(time.Hour), storetest.At)
	storetest.NoError(t, err)
	ctx := context.Background()
	storetest.NoError(t, stores.Charges.Save(ctx, charge, transfer.IdempotencyKey("a"), "fingerprint"))
	storetest.ErrorIs(t, stores.Charges.Save(ctx, charge, transfer.IdempotencyKey("b"), "fingerprint"), memory.ErrDuplicateID)
}

func TestRecord_ConcurrentFirstCallers_OnlyOneWins(t *testing.T) {
	stores := newPaymentStores(t)
	var group sync.WaitGroup
	wins := make(chan bool, concurrentWorkers)
	for range concurrentWorkers {
		group.Go(func() {
			err := stores.Unit.Run(context.Background(), func(ctx context.Context, store paymentservice.Store) error {
				first, err := store.Events.Record(ctx, "stub", "event-1")
				wins <- first
				return err
			})
			storetest.NoError(t, err)
		})
	}
	group.Wait()
	close(wins)
	total := 0
	for won := range wins {
		if won {
			total++
		}
	}
	storetest.Equal(t, total, 1)
}

func TestCheckBalances_CorruptedAccount_ReportsMismatchesInOrder(t *testing.T) {
	store := memory.NewStore(storetest.FixedClock{})
	checker := memory.NewLedgerRepository(store)
	store.CorruptBalance("01900000-0000-7000-8000-000000000002", 5)
	store.CorruptBalance(storetest.SettlementBRL, 7)
	mismatches, accounts, err := checker.CheckBalances(context.Background())
	storetest.NoError(t, err)
	storetest.Equal(t, accounts, 5)
	storetest.Equal(t, len(mismatches), 2)
	storetest.Equal(t, mismatches[0].AccountID, string(storetest.SettlementBRL))
	storetest.Equal(t, mismatches[0].StoredMinor, 7)
	storetest.Equal(t, mismatches[0].ComputedMinor, 0)
}
