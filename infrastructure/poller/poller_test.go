package poller_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/poller"
)

const (
	interval = 10 * time.Millisecond
	drain    = time.Second
	patience = 5 * time.Second
)

var errTask = errors.New("task failed")

type report struct {
	task string
	err  error
}

type spyReporter struct {
	reports chan report
}

func newSpyReporter() *spyReporter {
	return &spyReporter{reports: make(chan report, 16)}
}

func (r *spyReporter) Report(_ context.Context, task string, err error) {
	select {
	case r.reports <- report{task: task, err: err}:
	default:
	}
}

func receive[T any](t *testing.T, source <-chan T) T {
	t.Helper()
	select {
	case value := <-source:
		return value
	case <-time.After(patience):
		t.Fatal("timed out waiting on a channel")
		panic("unreachable")
	}
}

func start(ctx context.Context, task poller.Task, reporter poller.ErrorReporter, limit time.Duration) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- poller.New(interval, reporter).Run(ctx, "sync", task, limit)
	}()
	return result
}

func signalling(calls chan<- struct{}) poller.Task {
	return func(context.Context) error {
		select {
		case calls <- struct{}{}:
		default:
		}
		return nil
	}
}

func TestRun_Started_RunsBeforeFirstTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 1)
	before := time.Now()
	long := poller.New(time.Hour, newSpyReporter())
	result := make(chan error, 1)
	go func() { result <- long.Run(ctx, "sync", signalling(calls), drain) }()

	receive(t, calls)

	if elapsed := time.Since(before); elapsed >= time.Hour {
		t.Fatalf("first call after %v", elapsed)
	}
	cancel()
	if err := receive(t, result); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_Ticks_RunsRepeatedly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 1)
	result := start(ctx, signalling(calls), newSpyReporter(), drain)
	for range 3 {
		receive(t, calls)
	}
	cancel()
	if err := receive(t, result); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_TaskFails_ReportsAndContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reporter := newSpyReporter()
	result := start(ctx, func(context.Context) error { return errTask }, reporter, drain)
	for range 2 {
		got := receive(t, reporter.reports)
		if got.task != "sync" || !errors.Is(got.err, errTask) {
			t.Fatalf("report = %+v, want sync and %v", got, errTask)
		}
	}
	cancel()
	if err := receive(t, result); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_CancelledDuringRun_FinishesInFlightRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	taskContextError := make(chan error, 1)
	result := start(ctx, func(runContext context.Context) error {
		close(started)
		<-release
		taskContextError <- runContext.Err()
		return nil
	}, newSpyReporter(), drain)
	receive(t, started)
	cancel()
	select {
	case err := <-result:
		t.Fatalf("Run() returned %v before the run finished", err)
	case <-time.After(interval):
	}
	close(release)
	if err := receive(t, taskContextError); err != nil {
		t.Fatalf("task context error = %v, want nil during the drain", err)
	}
	if err := receive(t, result); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_RunOutlivesDrain_ReturnsDrainTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	result := start(ctx, func(context.Context) error {
		close(started)
		<-release
		return nil
	}, newSpyReporter(), interval)
	receive(t, started)
	cancel()
	if err := receive(t, result); !errors.Is(err, poller.ErrDrainTimeout) {
		t.Fatalf("Run() error = %v, want %v", err, poller.ErrDrainTimeout)
	}
}

func TestRun_Returned_LeavesNoGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 1)
	result := start(ctx, signalling(calls), newSpyReporter(), drain)
	receive(t, calls)
	cancel()
	receive(t, result)
	deadline := time.Now().Add(patience)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := runtime.NumGoroutine(); got > baseline {
		t.Fatalf("goroutines = %d, want at most %d", got, baseline)
	}
}
