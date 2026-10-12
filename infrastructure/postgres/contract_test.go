//go:build integration

package postgres_test

import (
	"testing"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/internal/storetest"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
)

func newPostgresStores(t *testing.T) storetest.Stores {
	t.Helper()
	pool := newTestPool(t)
	return storetest.Stores{
		Accounts:  postgres.NewAccountRepository(pool),
		Ledger:    postgres.NewLedgerRepository(pool),
		Transfers: postgres.NewTransferRepository(pool),
		Unit:      postgres.NewTransferUnitOfWork(pool),
	}
}

func TestAccountContract_Postgres_Passes(t *testing.T) {
	storetest.RunAccountContract(t, newPostgresStores)
}

func TestPosting_BuildJournalContract_Postgres_Passes(t *testing.T) {
	storetest.RunTransferContract(t, newPostgresStores)
}

func TestAPIKeyContract_Postgres_Passes(t *testing.T) {
	storetest.RunAPIKeyContract(t, func(t *testing.T) apikeyservice.Repository {
		return postgres.NewAPIKeyRepository(newTestPool(t))
	})
}

func TestWebhookContract_Postgres_Passes(t *testing.T) {
	storetest.RunWebhookContract(t, func(t *testing.T) storetest.WebhookStores {
		pool := newTestPool(t)
		return storetest.WebhookStores{
			Endpoints:  postgres.NewEndpointRepository(pool),
			Deliveries: postgres.NewDeliveryRepository(pool),
			Outbox:     postgres.NewOutboxRepository(pool),
			Unit:       postgres.NewWebhookUnitOfWork(pool),
			Dispatch:   postgres.NewDispatchUnitOfWork(pool, &storetest.SequentialIDs{}),
		}
	})
}

func TestPaymentContract_Postgres_Passes(t *testing.T) {
	storetest.RunPaymentContract(t, func(t *testing.T) storetest.PaymentStores {
		pool := newTestPool(t)
		return storetest.PaymentStores{
			Base: storetest.Stores{
				Accounts:  postgres.NewAccountRepository(pool),
				Ledger:    postgres.NewLedgerRepository(pool),
				Transfers: postgres.NewTransferRepository(pool),
				Unit:      postgres.NewTransferUnitOfWork(pool),
			},
			Charges: postgres.NewChargeRepository(pool),
			Payouts: postgres.NewPayoutRepository(pool),
			Events:  postgres.NewProviderEventLog(pool),
			Unit:    postgres.NewPaymentUnitOfWork(pool),
			Checker: postgres.NewLedgerRepository(pool),
		}
	})
}
