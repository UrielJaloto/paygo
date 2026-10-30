package money

import (
	"errors"
	"fmt"
)

type Currency string

const (
	BRL Currency = "BRL"
	USD Currency = "USD"
	EUR Currency = "EUR"
)

var ErrUnsupportedCurrency = errors.New("unsupported currency")

func ParseCurrency(code string) (Currency, error) {
	switch currency := Currency(code); currency {
	case BRL, USD, EUR:
		return currency, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedCurrency, code)
	}
}
