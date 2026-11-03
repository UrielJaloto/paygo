package poller_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/UrielJaloto/paygo/internal/infrastructure/poller"
)

func TestRunAll_EveryFunctionSucceeds_RunsAllAndReturnsNil(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context) error {
		calls.Add(1)
		return nil
	}
	if err := poller.RunAll(context.Background(), count, count, count); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRunAll_FunctionsFail_JoinsEveryError(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	err := poller.RunAll(context.Background(),
		func(context.Context) error { return first },
		func(context.Context) error { return second },
	)
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Errorf("err = %v, want both failures", err)
	}
}

func TestRunAll_OneFailsEarly_WaitsForTheOthersUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failure := errors.New("failure")
	var finished atomic.Bool
	err := poller.RunAll(ctx,
		func(context.Context) error {
			cancel()
			return failure
		},
		func(ctx context.Context) error {
			<-ctx.Done()
			finished.Store(true)
			return nil
		},
	)
	if !errors.Is(err, failure) || !finished.Load() {
		t.Errorf("err = %v, finished = %v", err, finished.Load())
	}
}
