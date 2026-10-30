package memory

import (
	"context"
	"fmt"
	"slices"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
)

type AccountRepository struct {
	store *Store
	tx    *transaction
}

func NewAccountRepository(store *Store) *AccountRepository {
	return &AccountRepository{store: store}
}

func (r *AccountRepository) Save(_ context.Context, acc account.Account) error {
	r.store.data.Lock()
	defer r.store.data.Unlock()
	if _, taken := r.store.accounts[acc.ID()]; taken {
		return fmt.Errorf("%w: account %s", ErrDuplicateID, acc.ID())
	}
	r.store.accounts[acc.ID()] = &accountRecord{account: acc}
	return nil
}

func (r *AccountRepository) FindByID(_ context.Context, id account.ID) (account.Account, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	record, found := r.store.accounts[id]
	if !found {
		return account.Account{}, account.ErrAccountNotFound
	}
	return record.account, nil
}

func (r *AccountRepository) ListByOwner(_ context.Context, owner apikey.OwnerID, cursor page.Cursor, limit page.Limit) ([]account.Account, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	var owned []account.Account
	for _, record := range r.store.accounts {
		if record.account.IsOwnedBy(owner) && record.account.Kind() == account.KindCustomer {
			owned = append(owned, record.account)
		}
	}
	return listing[account.Account]{id: accountIDText, cursor: cursor, limit: limit}.oldestFirst(owned), nil
}

func (r *AccountRepository) Update(_ context.Context, acc account.Account, expectedVersion int64) error {
	r.store.data.Lock()
	defer r.store.data.Unlock()
	record, found := r.store.accounts[acc.ID()]
	if !found || record.account.Version() != expectedVersion {
		return account.ErrConcurrentModification
	}
	record.account = acc
	return nil
}

func (r *AccountRepository) LockByIDs(_ context.Context, ids []account.ID) ([]account.Account, error) {
	unique := slices.Compact(slices.Sorted(slices.Values(ids)))
	records := r.store.accountRecords(unique)
	if len(records) < len(ids) {
		return nil, account.ErrAccountNotFound
	}
	for _, record := range records {
		r.tx.acquire(&record.lock)
	}
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	locked := make([]account.Account, 0, len(records))
	for _, record := range records {
		locked = append(locked, record.account)
	}
	return locked, nil
}

func accountIDText(acc account.Account) string {
	return string(acc.ID())
}
