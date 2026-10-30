package ledger_test

import (
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
)

func BenchmarkCalculateBalance(b *testing.B) {
	entries := make([]ledger.Entry, 10_000)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range entries {
		created, err := ledger.NewEntry("entry", "account", ledger.Credit, money.NewMoney(1, money.BRL), at)
		if err != nil {
			b.Fatal(err)
		}
		entries[i] = created
	}
	for b.Loop() {
		if _, err := ledger.CalculateBalance(money.BRL, entries); err != nil {
			b.Fatal(err)
		}
	}
}
