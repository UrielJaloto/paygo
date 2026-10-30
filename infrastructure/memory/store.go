package memory

import (
	"errors"
	"sync"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

const seedVersion int64 = 1

var ErrDuplicateID = errors.New("duplicate id")

type Clock interface {
	Now() time.Time
}

type accountRecord struct {
	lock      sync.Mutex
	account   account.Account
	balance   int64
	entries   []ledger.Entry
	transfers []transfer.ID
}

type transferRecord struct {
	lock        sync.Mutex
	transfer    transfer.Transfer
	fingerprint transfer.Fingerprint
}

type idempotencyScope struct {
	owner apikey.OwnerID
	key   transfer.IdempotencyKey
}

type Store struct {
	clock     Clock
	data      sync.RWMutex
	accounts  map[account.ID]*accountRecord
	transfers map[transfer.ID]*transferRecord
	keys      map[idempotencyScope]transfer.ID
	events    []event.Event
}

func NewStore(clock Clock) *Store {
	store := &Store{
		clock:     clock,
		accounts:  map[account.ID]*accountRecord{},
		transfers: map[transfer.ID]*transferRecord{},
		keys:      map[idempotencyScope]transfer.ID{},
	}
	store.seed("01900000-0000-7000-8000-000000000001", money.BRL, account.KindSettlement)
	store.seed("01900000-0000-7000-8000-000000000002", money.USD, account.KindSettlement)
	store.seed("01900000-0000-7000-8000-000000000003", money.EUR, account.KindSettlement)
	store.seed("01900000-0000-7000-8000-000000000011", money.BRL, account.KindProviderClearing)
	store.seed("01900000-0000-7000-8000-000000000021", money.BRL, account.KindPayoutHold)
	return store
}

func (s *Store) seed(id account.ID, currency money.Currency, kind account.Kind) {
	s.accounts[id] = &accountRecord{account: account.Restore(account.Snapshot{
		ID:        id,
		OwnerID:   apikey.SystemOwnerID,
		Currency:  currency,
		Kind:      kind,
		Status:    account.StatusActive,
		Version:   seedVersion,
		CreatedAt: s.clock.Now(),
	})}
}

func (s *Store) accountRecords(ids []account.ID) []*accountRecord {
	s.data.RLock()
	defer s.data.RUnlock()
	records := make([]*accountRecord, 0, len(ids))
	for _, id := range ids {
		if record, found := s.accounts[id]; found {
			records = append(records, record)
		}
	}
	return records
}

func (s *Store) write(tx *transaction, work func(tx *transaction) error) error {
	if tx != nil {
		return work(tx)
	}
	own := s.begin()
	defer own.release()
	if err := work(own); err != nil {
		return err
	}
	return own.commit()
}
