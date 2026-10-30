package money

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	MaximumAmountMinor int64 = 100_000_000_00

	minorPerUnit = 100
)

var (
	ErrInvalidAmount    = errors.New("invalid amount")
	ErrAmountTooLarge   = errors.New("amount too large")
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrAmountOverflow   = errors.New("amount overflow")
)

var amountPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)

type Money struct {
	minor    int64
	currency Currency
}

func NewMoney(minor int64, currency Currency) Money {
	return Money{minor: minor, currency: currency}
}

func NewAmount(minor int64, currency Currency) (Money, error) {
	if minor < 1 {
		return Money{}, fmt.Errorf("%w: %d minor units", ErrInvalidAmount, minor)
	}
	if minor > MaximumAmountMinor {
		return Money{}, fmt.Errorf("%w: %d minor units", ErrAmountTooLarge, minor)
	}
	return NewMoney(minor, currency), nil
}

func ParseAmount(text string, currency Currency) (Money, error) {
	if !amountPattern.MatchString(text) {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidAmount, text)
	}
	whole, fraction, _ := strings.Cut(text, ".")
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidAmount, text)
	}
	if units > MaximumAmountMinor/minorPerUnit {
		return Money{}, fmt.Errorf("%w: %q", ErrAmountTooLarge, text)
	}
	digits := fraction + "00"
	cents := int64(digits[0]-'0')*10 + int64(digits[1]-'0')
	return NewAmount(units*minorPerUnit+cents, currency)
}

func NewZero(currency Currency) Money {
	return NewMoney(0, currency)
}

func (m Money) Minor() int64 {
	return m.minor
}

func (m Money) Currency() Currency {
	return m.currency
}

func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	if other.minor > 0 && m.minor > math.MaxInt64-other.minor {
		return Money{}, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, m.minor, other.minor)
	}
	if other.minor < 0 && m.minor < math.MinInt64-other.minor {
		return Money{}, fmt.Errorf("%w: %d + %d", ErrAmountOverflow, m.minor, other.minor)
	}
	return NewMoney(m.minor+other.minor, m.currency), nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s and %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	if other.minor > 0 && m.minor < math.MinInt64+other.minor {
		return Money{}, fmt.Errorf("%w: %d - %d", ErrAmountOverflow, m.minor, other.minor)
	}
	if other.minor < 0 && m.minor > math.MaxInt64+other.minor {
		return Money{}, fmt.Errorf("%w: %d - %d", ErrAmountOverflow, m.minor, other.minor)
	}
	return NewMoney(m.minor-other.minor, m.currency), nil
}

func (m Money) Negate() Money {
	return NewMoney(-m.minor, m.currency)
}

func (m Money) IsNegative() bool {
	return m.minor < 0
}

func (m Money) IsZero() bool {
	return m.minor == 0
}

func (m Money) FormatDecimal() string {
	sign := ""
	magnitude := uint64(m.minor)
	if m.minor < 0 {
		sign = "-"
		magnitude = -magnitude
	}
	return fmt.Sprintf("%s%d.%02d", sign, magnitude/minorPerUnit, magnitude%minorPerUnit)
}

func (m Money) String() string {
	return m.FormatDecimal() + " " + string(m.currency)
}
