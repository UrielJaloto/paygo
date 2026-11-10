package ledgercheck

import (
	"context"
	"fmt"
	"slices"
)

type Mismatch struct {
	AccountID       string
	StoredMinor     int64
	CalculatedMinor int64
}

type Reader interface {
	ListBalanceMismatches(ctx context.Context) ([]Mismatch, int, error)
	CalculateTotalsByCurrency(ctx context.Context) (map[string]int64, error)
}

type Service struct {
	reader Reader
}

func NewService(reader Reader) *Service {
	return &Service{reader: reader}
}

func (s *Service) Check(ctx context.Context) (CheckOutput, error) {
	mismatches, accounts, err := s.reader.ListBalanceMismatches(ctx)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("checking balances: %w", err)
	}
	totals, err := s.reader.CalculateTotalsByCurrency(ctx)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("summing journal by currency: %w", err)
	}
	ids := collectMismatchedIDs(mismatches)
	currencies := collectUnbalancedCurrencies(totals)
	return CheckOutput{
		OK:                   len(ids) == 0 && len(currencies) == 0,
		Accounts:             accounts,
		Mismatches:           ids,
		UnbalancedCurrencies: currencies,
	}, nil
}

func collectMismatchedIDs(mismatches []Mismatch) []string {
	ids := make([]string, 0, len(mismatches))
	for _, mismatch := range mismatches {
		ids = append(ids, mismatch.AccountID)
	}
	slices.Sort(ids)
	return ids
}

func collectUnbalancedCurrencies(totals map[string]int64) []string {
	currencies := make([]string, 0, len(totals))
	for currency, sum := range totals {
		if sum != 0 {
			currencies = append(currencies, currency)
		}
	}
	slices.Sort(currencies)
	return currencies
}
