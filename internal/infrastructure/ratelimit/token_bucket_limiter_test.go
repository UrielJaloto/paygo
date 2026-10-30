package ratelimit_test

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/ratelimit"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) advance(duration time.Duration) {
	c.now = c.now.Add(duration)
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func allowances(limiter *ratelimit.TokenBucketLimiter, key string, calls int) []bool {
	results := make([]bool, 0, calls)
	for range calls {
		results = append(results, limiter.Allow(key))
	}
	return results
}

func TestAllow_Elapsed_RefillsBucket(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		calls   int
		want    []bool
	}{
		{name: "burst", elapsed: 0, calls: 1, want: []bool{false}},
		{name: "partial refill", elapsed: 500 * time.Millisecond, calls: 1, want: []bool{false}},
		{name: "full token", elapsed: time.Second, calls: 2, want: []bool{true, false}},
		{name: "cap at burst", elapsed: time.Hour, calls: 4, want: []bool{true, true, true, false}},
		{name: "clock moved back", elapsed: -time.Hour, calls: 1, want: []bool{false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newClock()
			limiter := ratelimit.NewMoney(clock, 1, 3)
			if got := allowances(limiter, "a", 3); !slices.Equal(got, []bool{true, true, true}) {
				t.Fatalf("initial allowances = %v, want all true", got)
			}
			clock.advance(test.elapsed)
			if got := allowances(limiter, "a", test.calls); !slices.Equal(got, test.want) {
				t.Fatalf("allowances = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAllow_KeyExhausted_OtherKeyAllowed(t *testing.T) {
	limiter := ratelimit.NewMoney(newClock(), 1, 1)
	limiter.Allow("a")
	if limiter.Allow("a") {
		t.Fatal("exhausted key a was allowed")
	}
	if !limiter.Allow("b") {
		t.Fatal("key b was denied")
	}
}

func TestAllow_IdleKey_RemovesBucket(t *testing.T) {
	clock := newClock()
	limiter := ratelimit.NewMoney(clock, 1, 1)
	limiter.Allow("a")
	clock.advance(11 * time.Minute)

	limiter.Allow("b")

	if got := limiter.BucketCount(); got != 1 {
		t.Fatalf("buckets = %d, want only the new key", got)
	}
}

func TestAllow_WithinSweepInterval_KeepsIdleBucket(t *testing.T) {
	clock := newClock()
	limiter := ratelimit.NewMoney(clock, 1, 1)
	limiter.Allow("a")
	clock.advance(9*time.Minute + 30*time.Second)
	limiter.Allow("b")
	clock.advance(45 * time.Second)

	limiter.Allow("b")

	if got := limiter.BucketCount(); got != 2 {
		t.Fatalf("buckets = %d, want the sweep skipped", got)
	}
}

func TestAllow_Concurrent_AllowsExactlyBurst(t *testing.T) {
	limiter := ratelimit.NewMoney(newClock(), 1, 50)
	var allowed atomic.Int64
	var running sync.WaitGroup
	for range 100 {
		running.Go(func() {
			if limiter.Allow("a") {
				allowed.Add(1)
			}
		})
	}
	running.Wait()
	if got := allowed.Load(); got != 50 {
		t.Fatalf("allowed = %d, want 50", got)
	}
}
