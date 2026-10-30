package money_test

import (
	"testing"

	"github.com/UrielJaloto/paygo/domain/money"
)

func BenchmarkParseAmount(b *testing.B) {
	for b.Loop() {
		if _, err := money.ParseAmount("12345.67", money.BRL); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAdd(b *testing.B) {
	left := money.New(1050, money.BRL)
	right := money.New(1, money.BRL)
	for b.Loop() {
		if _, err := left.Add(right); err != nil {
			b.Fatal(err)
		}
	}
}
