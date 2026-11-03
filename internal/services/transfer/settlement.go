package transfer

import (
	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

func settlementAccountFor(currency money.Currency) account.ID {
	return map[money.Currency]account.ID{
		money.BRL: "01900000-0000-7000-8000-000000000001",
		money.USD: "01900000-0000-7000-8000-000000000002",
		money.EUR: "01900000-0000-7000-8000-000000000003",
	}[currency]
}
