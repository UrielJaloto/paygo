package ledgercheck

import (
	"context"
	"fmt"
	"slices"
)

type Mismatch struct {
	AccountID     string
	StoredMinor   int64
	ComputedMinor int64
}

type Reader interface {
	CheckBalances(ctx context.Context) ([]Mismatch, int, error)
	SumByCurrency(ctx context.Context) (map[string]int64, error)
}

type Service struct {
	reader Reader
}

func New(reader Reader) *Service {
	return &Service{reader: reader}
}

func (s *Service) Check(ctx context.Context) (CheckOutput, error) {
	mismatches, accounts, err := s.reader.CheckBalances(ctx)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("checking balances: %w", err)
	}
	sums, err := s.reader.SumByCurrency(ctx)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("summing journal by currency: %w", err)
	}
	ids := mismatchedIDs(mismatches)
	currencies := unbalancedCurrencies(sums)
	return CheckOutput{
		OK:                   len(ids) == 0 && len(currencies) == 0,
		Accounts:             accounts,
		Mismatches:           ids,
		UnbalancedCurrencies: currencies,
	}, nil
}

func mismatchedIDs(mismatches []Mismatch) []string {
	ids := make([]string, 0, len(mismatches))
	for _, mismatch := range mismatches {
		ids = append(ids, mismatch.AccountID)
	}
	slices.Sort(ids)
	return ids
}

func unbalancedCurrencies(sums map[string]int64) []string {
	currencies := make([]string, 0, len(sums))
	for currency, sum := range sums {
		if sum != 0 {
			currencies = append(currencies, currency)
		}
	}
	slices.Sort(currencies)
	return currencies
}
