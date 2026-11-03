package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/money"
)

func TestParseCurrency_ReturnsCurrencyOrErrUnsupportedCurrency(t *testing.T) {
	cases := []struct {
		code    string
		want    money.Currency
		wantErr error
	}{
		{code: "BRL", want: money.BRL},
		{code: "USD", want: money.USD},
		{code: "EUR", want: money.EUR},
		{code: "brl", wantErr: money.ErrUnsupportedCurrency},
		{code: "", wantErr: money.ErrUnsupportedCurrency},
		{code: "GBP", wantErr: money.ErrUnsupportedCurrency},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			got, err := money.ParseCurrency(tc.code)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestParseAmount_ReturnsMinorUnitsOrExactError(t *testing.T) {
	cases := []struct {
		text    string
		want    int64
		wantErr error
	}{
		{text: "10", want: 1000},
		{text: "10.5", want: 1050},
		{text: "10.50", want: 1050},
		{text: "0.01", want: 1},
		{text: "100000000.00", want: 10000000000},
		{text: "100000000.01", wantErr: money.ErrAmountTooLarge},
		{text: "100000001", wantErr: money.ErrAmountTooLarge},
		{text: "0", wantErr: money.ErrInvalidAmount},
		{text: "0.00", wantErr: money.ErrInvalidAmount},
		{text: "", wantErr: money.ErrInvalidAmount},
		{text: " 10", wantErr: money.ErrInvalidAmount},
		{text: "10.", wantErr: money.ErrInvalidAmount},
		{text: ".5", wantErr: money.ErrInvalidAmount},
		{text: "-1", wantErr: money.ErrInvalidAmount},
		{text: "+1", wantErr: money.ErrInvalidAmount},
		{text: "1,00", wantErr: money.ErrInvalidAmount},
		{text: "10.505", wantErr: money.ErrInvalidAmount},
		{text: "1e3", wantErr: money.ErrInvalidAmount},
		{text: "abc", wantErr: money.ErrInvalidAmount},
		{text: "99999999999999999999", wantErr: money.ErrInvalidAmount},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got, err := money.ParseAmount(tc.text, money.BRL)
			if got != amountOrZero(tc.want) || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got (%v, %v), want (%d, %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func amountOrZero(minor int64) money.Money {
	if minor == 0 {
		return money.Money{}
	}
	return money.New(minor, money.BRL)
}

func TestNewAmount_EnforcesRange(t *testing.T) {
	cases := []struct {
		name    string
		minor   int64
		wantErr error
	}{
		{name: "negative", minor: -1, wantErr: money.ErrInvalidAmount},
		{name: "zero", minor: 0, wantErr: money.ErrInvalidAmount},
		{name: "minimum", minor: 1},
		{name: "maximum", minor: money.MaxAmountMinor},
		{name: "above maximum", minor: money.MaxAmountMinor + 1, wantErr: money.ErrAmountTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := money.NewAmount(tc.minor, money.BRL)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got != money.New(tc.minor, money.BRL) {
				t.Fatalf("got %v, want %d BRL", got, tc.minor)
			}
		})
	}
}

func TestAdd_ReturnsSumOrExactError(t *testing.T) {
	cases := []struct {
		name    string
		left    money.Money
		right   money.Money
		want    money.Money
		wantErr error
	}{
		{name: "same currency", left: money.New(1050, money.BRL), right: money.New(1, money.BRL), want: money.New(1051, money.BRL)},
		{name: "negative operand", left: money.New(1, money.BRL), right: money.New(-2, money.BRL), want: money.New(-1, money.BRL)},
		{name: "reaches maximum", left: money.New(math.MaxInt64-1, money.BRL), right: money.New(1, money.BRL), want: money.New(math.MaxInt64, money.BRL)},
		{name: "reaches minimum", left: money.New(math.MinInt64+1, money.BRL), right: money.New(-1, money.BRL), want: money.New(math.MinInt64, money.BRL)},
		{name: "mismatch", left: money.New(1, money.BRL), right: money.New(1, money.USD), wantErr: money.ErrCurrencyMismatch},
		{name: "overflow", left: money.New(math.MaxInt64, money.BRL), right: money.New(1, money.BRL), wantErr: money.ErrAmountOverflow},
		{name: "underflow", left: money.New(math.MinInt64, money.BRL), right: money.New(-1, money.BRL), wantErr: money.ErrAmountOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.left.Add(tc.right)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestSubtract_ReturnsDifferenceOrExactError(t *testing.T) {
	cases := []struct {
		name    string
		left    money.Money
		right   money.Money
		want    money.Money
		wantErr error
	}{
		{name: "to negative", left: money.New(1, money.BRL), right: money.New(2, money.BRL), want: money.New(-1, money.BRL)},
		{name: "negative operand", left: money.New(1, money.BRL), right: money.New(-2, money.BRL), want: money.New(3, money.BRL)},
		{name: "reaches minimum", left: money.New(math.MinInt64+1, money.BRL), right: money.New(1, money.BRL), want: money.New(math.MinInt64, money.BRL)},
		{name: "reaches maximum", left: money.New(math.MaxInt64-1, money.BRL), right: money.New(-1, money.BRL), want: money.New(math.MaxInt64, money.BRL)},
		{name: "mismatch", left: money.New(1, money.BRL), right: money.New(1, money.USD), wantErr: money.ErrCurrencyMismatch},
		{name: "underflow", left: money.New(math.MinInt64, money.BRL), right: money.New(1, money.BRL), wantErr: money.ErrAmountOverflow},
		{name: "overflow", left: money.New(math.MaxInt64, money.BRL), right: money.New(-1, money.BRL), wantErr: money.ErrAmountOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.left.Subtract(tc.right)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestPredicates_ReflectSign(t *testing.T) {
	cases := []struct {
		name         string
		value        money.Money
		wantNegative bool
		wantZero     bool
	}{
		{name: "negative", value: money.New(-1, money.BRL), wantNegative: true},
		{name: "zero", value: money.Zero(money.BRL), wantZero: true},
		{name: "positive", value: money.New(1, money.BRL)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value.IsNegative() != tc.wantNegative || tc.value.IsZero() != tc.wantZero {
				t.Fatalf("IsNegative %v IsZero %v", tc.value.IsNegative(), tc.value.IsZero())
			}
		})
	}
}

func TestNegate_FlipsSignAndKeepsCurrency(t *testing.T) {
	got := money.New(5, money.USD).Negate()
	if got.Minor() != -5 || got.Currency() != money.USD {
		t.Fatalf("got %v, want -5 USD", got)
	}
}

func TestFormatting_FollowsTwoDecimalFormat(t *testing.T) {
	cases := []struct {
		value       money.Money
		wantDecimal string
		wantString  string
	}{
		{value: money.New(1050, money.BRL), wantDecimal: "10.50", wantString: "10.50 BRL"},
		{value: money.New(-1, money.BRL), wantDecimal: "-0.01", wantString: "-0.01 BRL"},
		{value: money.Zero(money.USD), wantDecimal: "0.00", wantString: "0.00 USD"},
		{value: money.New(100000, money.EUR), wantDecimal: "1000.00", wantString: "1000.00 EUR"},
		{value: money.New(math.MinInt64, money.BRL), wantDecimal: "-92233720368547758.08", wantString: "-92233720368547758.08 BRL"},
	}
	for _, tc := range cases {
		t.Run(tc.wantString, func(t *testing.T) {
			if tc.value.Decimal() != tc.wantDecimal || tc.value.String() != tc.wantString {
				t.Fatalf("Decimal %q String %q", tc.value.Decimal(), tc.value.String())
			}
		})
	}
}
