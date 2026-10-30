package clock_test

import (
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/clock"
)

func TestNow_AnyInstant_ReturnsUTCMilliseconds(t *testing.T) {
	now := clock.New().Now()
	if now.Location() != time.UTC {
		t.Fatalf("location = %v, want UTC", now.Location())
	}
	if now.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatalf("nanoseconds = %d, want a multiple of 1e6", now.Nanosecond())
	}
}
