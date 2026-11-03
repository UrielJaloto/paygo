package poller

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrDrainTimeout = errors.New("poller drain timeout")

type Task func(ctx context.Context) error

type ErrorReporter interface {
	Report(ctx context.Context, task string, err error)
}

type Poller struct {
	interval time.Duration
	reporter ErrorReporter
}

type run struct {
	name  string
	task  Task
	drain time.Duration
}

func New(interval time.Duration, reporter ErrorReporter) *Poller {
	return &Poller{interval: interval, reporter: reporter}
}

func (p *Poller) Run(ctx context.Context, name string, task Task, drain time.Duration) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := p.runOnce(ctx, run{name: name, task: task, drain: drain}); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

func (p *Poller) runOnce(ctx context.Context, current run) error {
	detached := context.WithoutCancel(ctx)
	runContext, cancel := context.WithCancel(detached)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := current.task(runContext); err != nil {
			p.reporter.Report(detached, current.name, err)
		}
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return awaitDrain(done, current)
	}
}

func awaitDrain(done <-chan struct{}, current run) error {
	timer := time.NewTimer(current.drain)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w: %s", ErrDrainTimeout, current.name)
	}
}
