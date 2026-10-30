package payment

import (
	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/money"
)

func clearingAccountFor(currency money.Currency) account.ID {
	return map[money.Currency]account.ID{
		money.BRL: "01900000-0000-7000-8000-000000000011",
	}[currency]
}

func holdAccountFor(currency money.Currency) account.ID {
	return map[money.Currency]account.ID{
		money.BRL: "01900000-0000-7000-8000-000000000021",
	}[currency]
}
