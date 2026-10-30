package memory

import (
	"sync"

	"github.com/UrielJaloto/paygo/domain/account"
)

type transaction struct {
	store  *Store
	held   map[*sync.Mutex]struct{}
	order  []*sync.Mutex
	deltas map[account.ID]int64
	checks []func() error
	writes []func()
}

func (s *Store) begin() *transaction {
	return &transaction{store: s, held: map[*sync.Mutex]struct{}{}, deltas: map[account.ID]int64{}}
}

func (t *transaction) acquire(lock *sync.Mutex) {
	if t == nil {
		return
	}
	if _, owned := t.held[lock]; owned {
		return
	}
	lock.Lock()
	t.held[lock] = struct{}{}
	t.order = append(t.order, lock)
}

func (t *transaction) tryAcquire(lock *sync.Mutex) bool {
	if t == nil {
		return true
	}
	if _, owned := t.held[lock]; owned {
		return true
	}
	if !lock.TryLock() {
		return false
	}
	t.held[lock] = struct{}{}
	t.order = append(t.order, lock)
	return true
}

func (t *transaction) pendingDelta(id account.ID) int64 {
	if t == nil {
		return 0
	}
	return t.deltas[id]
}

func (t *transaction) commit() error {
	t.store.data.Lock()
	defer t.store.data.Unlock()
	for _, check := range t.checks {
		if err := check(); err != nil {
			return err
		}
	}
	for _, write := range t.writes {
		write()
	}
	return nil
}

func (t *transaction) release() {
	for _, lock := range t.order {
		lock.Unlock()
	}
}
