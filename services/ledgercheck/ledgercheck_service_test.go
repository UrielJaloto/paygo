package ledgercheck_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	service "github.com/UrielJaloto/paygo/services/ledgercheck"
)

var errBoom = errors.New("boom")

type stubReader struct {
	mismatches []service.Mismatch
	accounts   int
	sums       map[string]int64
	balanceErr error
	sumErr     error
}

func (s stubReader) CheckBalances(context.Context) ([]service.Mismatch, int, error) {
	return s.mismatches, s.accounts, s.balanceErr
}

func (s stubReader) SumByCurrency(context.Context) (map[string]int64, error) {
	return s.sums, s.sumErr
}

func TestCheck_ReaderSucceeds_ReportsLedgerHealth(t *testing.T) {
	tests := []struct {
		name       string
		reader     stubReader
		ok         bool
		accounts   int
		mismatches []string
		currencies []string
	}{
		{
			name:       "all good",
			reader:     stubReader{accounts: 3, sums: map[string]int64{"BRL": 0, "USD": 0}},
			ok:         true,
			accounts:   3,
			mismatches: []string{},
			currencies: []string{},
		},
		{
			name: "one mismatch",
			reader: stubReader{
				accounts:   2,
				mismatches: []service.Mismatch{{AccountID: "a", StoredMinor: 10, ComputedMinor: 9}},
				sums:       map[string]int64{"BRL": 0},
			},
			accounts:   2,
			mismatches: []string{"a"},
			currencies: []string{},
		},
		{
			name:       "currency does not sum to zero",
			reader:     stubReader{accounts: 1, sums: map[string]int64{"BRL": 5}},
			accounts:   1,
			mismatches: []string{},
			currencies: []string{"BRL"},
		},
		{
			name: "results are sorted ascending",
			reader: stubReader{
				accounts:   4,
				mismatches: []service.Mismatch{{AccountID: "b"}, {AccountID: "a"}},
				sums:       map[string]int64{"USD": -1, "BRL": 5, "EUR": 0},
			},
			accounts:   4,
			mismatches: []string{"a", "b"},
			currencies: []string{"BRL", "USD"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := service.New(tc.reader).Check(context.Background())
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if got.OK != tc.ok || got.Accounts != tc.accounts {
				t.Errorf("Check() = %+v, want OK %v and accounts %d", got, tc.ok, tc.accounts)
			}
			if !slices.Equal(got.Mismatches, tc.mismatches) {
				t.Errorf("Mismatches = %v, want %v", got.Mismatches, tc.mismatches)
			}
			if !slices.Equal(got.UnbalancedCurrencies, tc.currencies) {
				t.Errorf("UnbalancedCurrencies = %v, want %v", got.UnbalancedCurrencies, tc.currencies)
			}
		})
	}
}

func TestCheck_ReaderFails_ReturnsWrappedError(t *testing.T) {
	tests := []struct {
		name   string
		reader stubReader
	}{
		{name: "check balances", reader: stubReader{balanceErr: errBoom}},
		{name: "sum by currency", reader: stubReader{sumErr: errBoom}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.New(tc.reader).Check(context.Background())
			if !errors.Is(err, errBoom) {
				t.Fatalf("Check() error = %v, want %v", err, errBoom)
			}
			if err.Error() == errBoom.Error() {
				t.Errorf("Check() error = %q, want it wrapped", err)
			}
		})
	}
}
