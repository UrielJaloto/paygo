package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/UrielJaloto/paygo/domain/money"
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
	for _, testCase := range cases {
		t.Run(testCase.code, func(t *testing.T) {
			got, err := money.ParseCurrency(testCase.code)
			if got != testCase.want || !errors.Is(err, testCase.wantErr) {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, err, testCase.want, testCase.wantErr)
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
	for _, testCase := range cases {
		t.Run(testCase.text, func(t *testing.T) {
			got, err := money.ParseAmount(testCase.text, money.BRL)
			if got != amountOrZero(testCase.want) || !errors.Is(err, testCase.wantErr) {
				t.Fatalf("got (%v, %v), want (%d, %v)", got, err, testCase.want, testCase.wantErr)
			}
		})
	}
}

func amountOrZero(minor int64) money.Money {
	if minor == 0 {
		return money.Money{}
	}
	return money.NewMoney(minor, money.BRL)
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
		{name: "maximum", minor: money.MaximumAmountMinor},
		{name: "above maximum", minor: money.MaximumAmountMinor + 1, wantErr: money.ErrAmountTooLarge},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := money.NewAmount(testCase.minor, money.BRL)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if testCase.wantErr == nil && got != money.NewMoney(testCase.minor, money.BRL) {
				t.Fatalf("got %v, want %d BRL", got, testCase.minor)
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
		{name: "same currency", left: money.NewMoney(1050, money.BRL), right: money.NewMoney(1, money.BRL), want: money.NewMoney(1051, money.BRL)},
		{name: "negative operand", left: money.NewMoney(1, money.BRL), right: money.NewMoney(-2, money.BRL), want: money.NewMoney(-1, money.BRL)},
		{name: "reaches maximum", left: money.NewMoney(math.MaxInt64-1, money.BRL), right: money.NewMoney(1, money.BRL), want: money.NewMoney(math.MaxInt64, money.BRL)},
		{name: "reaches minimum", left: money.NewMoney(math.MinInt64+1, money.BRL), right: money.NewMoney(-1, money.BRL), want: money.NewMoney(math.MinInt64, money.BRL)},
		{name: "mismatch", left: money.NewMoney(1, money.BRL), right: money.NewMoney(1, money.USD), wantErr: money.ErrCurrencyMismatch},
		{name: "overflow", left: money.NewMoney(math.MaxInt64, money.BRL), right: money.NewMoney(1, money.BRL), wantErr: money.ErrAmountOverflow},
		{name: "underflow", left: money.NewMoney(math.MinInt64, money.BRL), right: money.NewMoney(-1, money.BRL), wantErr: money.ErrAmountOverflow},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := testCase.left.Add(testCase.right)
			if got != testCase.want || !errors.Is(err, testCase.wantErr) {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, err, testCase.want, testCase.wantErr)
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
		{name: "to negative", left: money.NewMoney(1, money.BRL), right: money.NewMoney(2, money.BRL), want: money.NewMoney(-1, money.BRL)},
		{name: "negative operand", left: money.NewMoney(1, money.BRL), right: money.NewMoney(-2, money.BRL), want: money.NewMoney(3, money.BRL)},
		{name: "reaches minimum", left: money.NewMoney(math.MinInt64+1, money.BRL), right: money.NewMoney(1, money.BRL), want: money.NewMoney(math.MinInt64, money.BRL)},
		{name: "reaches maximum", left: money.NewMoney(math.MaxInt64-1, money.BRL), right: money.NewMoney(-1, money.BRL), want: money.NewMoney(math.MaxInt64, money.BRL)},
		{name: "mismatch", left: money.NewMoney(1, money.BRL), right: money.NewMoney(1, money.USD), wantErr: money.ErrCurrencyMismatch},
		{name: "underflow", left: money.NewMoney(math.MinInt64, money.BRL), right: money.NewMoney(1, money.BRL), wantErr: money.ErrAmountOverflow},
		{name: "overflow", left: money.NewMoney(math.MaxInt64, money.BRL), right: money.NewMoney(-1, money.BRL), wantErr: money.ErrAmountOverflow},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := testCase.left.Subtract(testCase.right)
			if got != testCase.want || !errors.Is(err, testCase.wantErr) {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, err, testCase.want, testCase.wantErr)
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
		{name: "negative", value: money.NewMoney(-1, money.BRL), wantNegative: true},
		{name: "zero", value: money.NewZero(money.BRL), wantZero: true},
		{name: "positive", value: money.NewMoney(1, money.BRL)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.value.IsNegative() != testCase.wantNegative || testCase.value.IsZero() != testCase.wantZero {
				t.Fatalf("IsNegative %v IsZero %v", testCase.value.IsNegative(), testCase.value.IsZero())
			}
		})
	}
}

func TestNegate_FlipsSignAndKeepsCurrency(t *testing.T) {
	got := money.NewMoney(5, money.USD).Negate()
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
		{value: money.NewMoney(1050, money.BRL), wantDecimal: "10.50", wantString: "10.50 BRL"},
		{value: money.NewMoney(-1, money.BRL), wantDecimal: "-0.01", wantString: "-0.01 BRL"},
		{value: money.NewZero(money.USD), wantDecimal: "0.00", wantString: "0.00 USD"},
		{value: money.NewMoney(100000, money.EUR), wantDecimal: "1000.00", wantString: "1000.00 EUR"},
		{value: money.NewMoney(math.MinInt64, money.BRL), wantDecimal: "-92233720368547758.08", wantString: "-92233720368547758.08 BRL"},
	}
	for _, testCase := range cases {
		t.Run(testCase.wantString, func(t *testing.T) {
			if testCase.value.FormatDecimal() != testCase.wantDecimal || testCase.value.String() != testCase.wantString {
				t.Fatalf("Decimal %q String %q", testCase.value.FormatDecimal(), testCase.value.String())
			}
		})
	}
}
