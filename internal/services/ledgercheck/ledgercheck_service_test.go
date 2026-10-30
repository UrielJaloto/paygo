package ledgercheck_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	service "github.com/UrielJaloto/paygo/internal/services/ledgercheck"
)

var errBoom = errors.New("boom")

type stubReader struct {
	mismatches []service.Mismatch
	accounts   int
	totals       map[string]int64
	balanceErr error
	totalsErr     error
}

func (s stubReader) ListBalanceMismatches(context.Context) ([]service.Mismatch, int, error) {
	return s.mismatches, s.accounts, s.balanceErr
}

func (s stubReader) CalculateTotalsByCurrency(context.Context) (map[string]int64, error) {
	return s.totals, s.totalsErr
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
			reader:     stubReader{accounts: 3, totals: map[string]int64{"BRL": 0, "USD": 0}},
			ok:         true,
			accounts:   3,
			mismatches: []string{},
			currencies: []string{},
		},
		{
			name: "one mismatch",
			reader: stubReader{
				accounts:   2,
				mismatches: []service.Mismatch{{AccountID: "a", StoredMinor: 10, CalculatedMinor: 9}},
				totals:       map[string]int64{"BRL": 0},
			},
			accounts:   2,
			mismatches: []string{"a"},
			currencies: []string{},
		},
		{
			name:       "currency does not sum to zero",
			reader:     stubReader{accounts: 1, totals: map[string]int64{"BRL": 5}},
			accounts:   1,
			mismatches: []string{},
			currencies: []string{"BRL"},
		},
		{
			name: "results are sorted ascending",
			reader: stubReader{
				accounts:   4,
				mismatches: []service.Mismatch{{AccountID: "b"}, {AccountID: "a"}},
				totals:       map[string]int64{"USD": -1, "BRL": 5, "EUR": 0},
			},
			accounts:   4,
			mismatches: []string{"a", "b"},
			currencies: []string{"BRL", "USD"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := service.NewMoney(testCase.reader).Check(context.Background())
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if got.OK != testCase.ok || got.Accounts != testCase.accounts {
				t.Errorf("Check() = %+v, want OK %v and accounts %d", got, testCase.ok, testCase.accounts)
			}
			if !slices.Equal(got.Mismatches, testCase.mismatches) {
				t.Errorf("Mismatches = %v, want %v", got.Mismatches, testCase.mismatches)
			}
			if !slices.Equal(got.UnbalancedCurrencies, testCase.currencies) {
				t.Errorf("UnbalancedCurrencies = %v, want %v", got.UnbalancedCurrencies, testCase.currencies)
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
		{name: "sum by currency", reader: stubReader{totalsErr: errBoom}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := service.NewMoney(testCase.reader).Check(context.Background())
			if !errors.Is(err, errBoom) {
				t.Fatalf("Check() error = %v, want %v", err, errBoom)
			}
			if err.Error() == errBoom.Error() {
				t.Errorf("Check() error = %q, want it wrapped", err)
			}
		})
	}
}
