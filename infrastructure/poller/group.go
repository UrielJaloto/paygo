package poller

import (
	"context"
	"errors"
	"sync"
)

func RunAll(ctx context.Context, runs ...func(ctx context.Context) error) error {
	failures := make([]error, len(runs))
	var group sync.WaitGroup
	for index, run := range runs {
		group.Go(func() { failures[index] = run(ctx) })
	}
	group.Wait()
	return errors.Join(failures...)
}
