package money_test

import (
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/money"
)

func FuzzParseAmount(f *testing.F) {
	seeds := []string{
		"10", "10.5", "10.50", "0.01", "100000000.00", "100000000.01", "0", "0.00",
		"", " 10", "10.", ".5", "-1", "+1", "1,00", "10.505", "1e3", "abc", "99999999999999999999",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		parsed, err := money.ParseAmount(text, money.BRL)
		if err != nil {
			return
		}
		again, err := money.ParseAmount(parsed.Decimal(), money.BRL)
		if err != nil {
			t.Fatalf("Decimal %q does not parse back: %v", parsed.Decimal(), err)
		}
		if again.Minor() != parsed.Minor() {
			t.Fatalf("round trip of %q: got %d, want %d", text, again.Minor(), parsed.Minor())
		}
	})
}
