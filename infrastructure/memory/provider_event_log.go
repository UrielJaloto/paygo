package memory

import (
	"context"
	"sync"
)

type providerEventScope struct {
	provider string
	eventID  string
}

type providerEventRecord struct {
	lock     sync.Mutex
	recorded bool
}

type ProviderEventLog struct {
	store *Store
	transaction    *transaction
}

func NewProviderEventLog(store *Store) *ProviderEventLog {
	return &ProviderEventLog{store: store}
}

func (l *ProviderEventLog) Record(_ context.Context, provider, eventID string) (bool, error) {
	first := false
	err := l.store.write(l.transaction, func(transaction *transaction) error {
		record := l.store.providerEvent(providerEventScope{provider: provider, eventID: eventID})
		transaction.acquire(&record.lock)
		first = l.store.markProviderEvent(transaction, record)
		return nil
	})
	return first, err
}

func (s *Store) providerEvent(scope providerEventScope) *providerEventRecord {
	s.data.Lock()
	defer s.data.Unlock()
	record, found := s.payments.events[scope]
	if !found {
		record = &providerEventRecord{}
		s.payments.events[scope] = record
	}
	return record
}

func (s *Store) markProviderEvent(transaction *transaction, record *providerEventRecord) bool {
	s.data.RLock()
	defer s.data.RUnlock()
	if record.recorded {
		return false
	}
	transaction.writes = append(transaction.writes, func() { record.recorded = true })
	return true
}
