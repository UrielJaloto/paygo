package memory

import (
	"context"
	"slices"
	"strings"

	"github.com/UrielJaloto/paygo/services/ledgercheck"
)

func (r *LedgerRepository) CheckBalances(_ context.Context) ([]ledgercheck.Mismatch, int, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	mismatches := make([]ledgercheck.Mismatch, 0)
	for id, record := range r.store.accounts {
		computed := computedBalance(record)
		if computed != record.balance {
			mismatches = append(mismatches, ledgercheck.Mismatch{AccountID: string(id), StoredMinor: record.balance, ComputedMinor: computed})
		}
	}
	slices.SortFunc(mismatches, func(a, b ledgercheck.Mismatch) int {
		return strings.Compare(a.AccountID, b.AccountID)
	})
	return mismatches, len(r.store.accounts), nil
}

func (r *LedgerRepository) SumByCurrency(_ context.Context) (map[string]int64, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	sums := map[string]int64{}
	for _, record := range r.store.accounts {
		for _, entry := range record.entries {
			sums[string(entry.Amount().Currency())] += signedMinor(entry)
		}
	}
	return sums, nil
}

func computedBalance(record *accountRecord) int64 {
	var total int64
	for _, entry := range record.entries {
		total += signedMinor(entry)
	}
	return total
}
