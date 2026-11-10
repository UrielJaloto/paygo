package payment

import (
	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

func findClearingAccountID(currency money.Currency) account.ID {
	return map[money.Currency]account.ID{
		money.BRL: "01900000-0000-7000-8000-000000000011",
	}[currency]
}

func findHoldAccountID(currency money.Currency) account.ID {
	return map[money.Currency]account.ID{
		money.BRL: "01900000-0000-7000-8000-000000000021",
	}[currency]
}
