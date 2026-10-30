//go:build integration

package postgres_test

import (
	"testing"

	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
)

func newPostgresStores(t *testing.T) Stores {
	t.Helper()
	pool := newTestPool(t)
	return Stores{
		Accounts:  postgres.NewAccountRepository(pool),
		Ledger:    postgres.NewLedgerRepository(pool),
		Transfers: postgres.NewTransferRepository(pool),
		Unit:      postgres.NewTransferUnitOfWork(pool),
		Outbox:    postgres.NewOutboxRepository(pool),
	}
}

func TestAccountContract_Postgres_Passes(t *testing.T) {
	runAccountContract(t, newPostgresStores)
}

func TestPosting_BuildJournalContract_Postgres_Passes(t *testing.T) {
	runTransferContract(t, newPostgresStores)
}

func TestAPIKeyContract_Postgres_Passes(t *testing.T) {
	runAPIKeyContract(t, func(t *testing.T) apikeyservice.Repository {
		return postgres.NewAPIKeyRepository(newTestPool(t))
	})
}

func TestWebhookContract_Postgres_Passes(t *testing.T) {
	runWebhookContract(t, func(t *testing.T) WebhookStores {
		pool := newTestPool(t)
		return WebhookStores{
			Endpoints:  postgres.NewEndpointRepository(pool),
			Deliveries: postgres.NewDeliveryRepository(pool),
			Outbox:     postgres.NewOutboxRepository(pool),
			Unit:       postgres.NewWebhookUnitOfWork(pool),
			Dispatch:   postgres.NewDispatchUnitOfWork(pool, &sequentialIDs{}),
		}
	})
}

func TestPaymentContract_Postgres_Passes(t *testing.T) {
	runPaymentContract(t, func(t *testing.T) PaymentStores {
		pool := newTestPool(t)
		return PaymentStores{
			Base: Stores{
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
