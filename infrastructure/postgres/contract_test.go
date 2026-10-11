//go:build integration

package postgres_test

import (
	"testing"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/internal/storetest"
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
