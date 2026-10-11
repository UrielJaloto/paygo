package memory

import (
	"context"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
)

type LedgerRepository struct {
	store *Store
	transaction    *transaction
}

func NewLedgerRepository(store *Store) *LedgerRepository {
	return &LedgerRepository{store: store}
}

func (r *LedgerRepository) CalculateBalance(_ context.Context, id account.ID) (money.Money, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	record, found := r.store.accounts[id]
	if !found {
		return money.Money{}, account.ErrAccountNotFound
	}
	return money.NewMoney(record.balance+r.transaction.pendingDelta(id), record.account.Currency()), nil
}

func (r *LedgerRepository) ListStatement(_ context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]ledger.Entry, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	record, found := r.store.accounts[id]
	if !found {
		return nil, nil
	}
	return listing[ledger.Entry]{id: ledger.Entry.ID, cursor: cursor, limit: limit}.newestFirst(record.entries), nil
}

func (r *LedgerRepository) Append(_ context.Context, journal ledger.JournalTransaction) error {
	return r.store.write(r.transaction, func(transaction *transaction) error {
		return transaction.addJournal(journal)
	})
}

func (t *transaction) addJournal(journal ledger.JournalTransaction) error {
	entries := journal.Entries()
	for _, entry := range entries {
		records := t.store.accountRecords([]account.ID{entry.AccountID()})
		if len(records) == 0 {
			return account.ErrAccountNotFound
		}
		t.deltas[entry.AccountID()] += signedMinor(entry)
	}
	t.writes = append(t.writes, func() {
		for _, entry := range entries {
			record := t.store.accounts[entry.AccountID()]
			record.balance += signedMinor(entry)
			record.entries = append(record.entries, entry)
		}
	})
	return nil
}

func signedMinor(entry ledger.Entry) int64 {
	if entry.Direction() == ledger.Credit {
		return entry.Amount().Minor()
	}
	return -entry.Amount().Minor()
}
