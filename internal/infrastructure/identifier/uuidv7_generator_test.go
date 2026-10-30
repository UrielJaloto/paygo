package identifier_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/infrastructure/identifier"
)

var uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type fixedClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *fixedClock) set(now time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = now
}

type zeroReader struct{}

func (zeroReader) Read(target []byte) (int, error) {
	clear(target)
	return len(target), nil
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("no entropy")
}

func startOf2026() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func assertIncreasing(t *testing.T, generator *identifier.UUIDv7Generator, calls int) {
	t.Helper()
	previous := generator.NewID()
	for range calls {
		current := generator.NewID()
		if current <= previous {
			t.Fatalf("id %q is not after %q", current, previous)
		}
		previous = current
	}
}

func TestNewID_FixedInputs_MatchesLayout(t *testing.T) {
	timestamp := fmt.Sprintf("%012x", startOf2026().UnixMilli())
	want := timestamp[:8] + "-" + timestamp[8:] + "-7000-8000-000000000000"

	got := identifier.NewMoney(&fixedClock{now: startOf2026()}, zeroReader{}).NewID()

	if got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
	if !uuidV7Pattern.MatchString(got) {
		t.Fatalf("id %q is not a UUIDv7", got)
	}
}

func TestNewID_SameMillisecond_IsStrictlyIncreasing(t *testing.T) {
	assertIncreasing(t, identifier.NewMoney(&fixedClock{now: startOf2026()}, rand.Reader), 1000)
}

func TestNewID_CounterExhausted_StaysIncreasing(t *testing.T) {
	assertIncreasing(t, identifier.NewMoney(&fixedClock{now: startOf2026()}, zeroReader{}), 5000)
}

func TestNewID_ClockAdvances_ReseedsCounter(t *testing.T) {
	clock := &fixedClock{now: startOf2026()}
	generator := identifier.NewMoney(clock, zeroReader{})
	generator.NewID()
	second := generator.NewID()
	clock.set(startOf2026().Add(time.Millisecond))

	third := generator.NewID()

	if third <= second {
		t.Fatalf("id %q is not after %q", third, second)
	}
	if !strings.Contains(third, "-7000-") {
		t.Fatalf("id %q, want the counter reseeded", third)
	}
}

func TestNewID_ClockGoesBack_StaysIncreasing(t *testing.T) {
	clock := &fixedClock{now: startOf2026()}
	generator := identifier.NewMoney(clock, zeroReader{})
	first := generator.NewID()
	clock.set(startOf2026().Add(-time.Hour))

	second := generator.NewID()

	if second <= first {
		t.Fatalf("id %q is not after %q", second, first)
	}
}

func TestNewID_Concurrent_ReturnsUniqueIDs(t *testing.T) {
	generator := identifier.NewMoney(&fixedClock{now: startOf2026()}, rand.Reader)
	ids := make(chan string, 8000)
	var running sync.WaitGroup
	for range 8 {
		running.Go(func() {
			for range 1000 {
				ids <- generator.NewID()
			}
		})
	}
	running.Wait()
	close(ids)
	unique := make(map[string]struct{}, 8000)
	for id := range ids {
		unique[id] = struct{}{}
	}
	if len(unique) != 8000 {
		t.Fatalf("unique ids = %d, want 8000", len(unique))
	}
}

func TestNewID_RandomFails_Panics(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "generating uuid") {
			t.Fatalf("recovered = %v, want a generating uuid panic", recovered)
		}
	}()
	identifier.NewMoney(&fixedClock{now: startOf2026()}, failingReader{}).NewID()
}
