package randomness_test

import (
	"testing"

	"github.com/UrielJaloto/paygo/internal/infrastructure/randomness"
)

func TestNext_ManyValues_StaysInRange(t *testing.T) {
	jitter := randomness.NewJitter(1)
	for range 10000 {
		if value := jitter.Next(); value < -0.2 || value > 0.2 {
			t.Fatalf("value = %v, want within [-0.2, 0.2]", value)
		}
	}
}

func TestNext_SameSeed_ReturnsSameSequence(t *testing.T) {
	first, second := randomness.NewJitter(42), randomness.NewJitter(42)
	for range 5 {
		if a, b := first.Next(), second.Next(); a != b {
			t.Fatalf("values differ: %v and %v", a, b)
		}
	}
}
