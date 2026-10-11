package memory

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type TransferRepository struct {
	store *Store
	transaction    *transaction
}

func NewTransferRepository(store *Store) *TransferRepository {
	return &TransferRepository{store: store}
}

func (r *TransferRepository) FindByIdempotencyKey(_ context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (transfer.Transfer, transfer.Fingerprint, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	id, found := r.store.keys[idempotencyScope{owner: owner, key: key}]
	if !found {
		return transfer.Transfer{}, "", transfer.ErrTransferNotFound
	}
	record := r.store.transfers[id]
	return record.transfer, record.fingerprint, nil
}

func (r *TransferRepository) FindByIDForUpdate(ctx context.Context, id transfer.ID) (transfer.Transfer, error) {
	r.store.data.RLock()
	record, found := r.store.transfers[id]
	r.store.data.RUnlock()
	if !found {
		return transfer.Transfer{}, transfer.ErrTransferNotFound
	}
	r.transaction.acquire(&record.lock)
	return r.FindByID(ctx, id)
}

func (r *TransferRepository) FindByID(_ context.Context, id transfer.ID) (transfer.Transfer, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	record, found := r.store.transfers[id]
	if !found {
		return transfer.Transfer{}, transfer.ErrTransferNotFound
	}
	return record.transfer, nil
}

func (r *TransferRepository) ListByAccount(_ context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]transfer.Transfer, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	record, found := r.store.accounts[id]
	if !found {
		return nil, nil
	}
	moved := make([]transfer.Transfer, 0, len(record.transfers))
	for _, transferID := range record.transfers {
		moved = append(moved, r.store.transfers[transferID].transfer)
	}
	return listing[transfer.Transfer]{id: transferIDText, cursor: cursor, limit: limit}.newestFirst(moved), nil
}

func (r *TransferRepository) Save(_ context.Context, t transfer.Transfer, fingerprint transfer.Fingerprint) error {
	return r.store.write(r.transaction, func(transaction *transaction) error {
		return transaction.addTransfer(t, fingerprint)
	})
}

func (r *TransferRepository) MarkReversed(_ context.Context, t transfer.Transfer) error {
	return r.store.write(r.transaction, func(transaction *transaction) error {
		transaction.writes = append(transaction.writes, func() { transaction.store.markReversed(t.ID()) })
		return nil
	})
}

func (t *transaction) addTransfer(created transfer.Transfer, fingerprint transfer.Fingerprint) error {
	check := func() error { return t.store.checkTransfer(created) }
	t.store.data.RLock()
	err := check()
	t.store.data.RUnlock()
	if err != nil {
		return err
	}
	t.checks = append(t.checks, check)
	t.writes = append(t.writes, func() { t.store.insertTransfer(created, fingerprint) })
	return nil
}

func (s *Store) checkTransfer(t transfer.Transfer) error {
	if _, taken := s.keys[scopeOf(t)]; taken {
		return fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, t.IdempotencyKey())
	}
	if s.accounts[t.From()] == nil || s.accounts[t.To()] == nil {
		return account.ErrAccountNotFound
	}
	return nil
}

func (s *Store) insertTransfer(t transfer.Transfer, fingerprint transfer.Fingerprint) {
	s.transfers[t.ID()] = &transferRecord{transfer: t, fingerprint: fingerprint}
	s.keys[scopeOf(t)] = t.ID()
	from, to := s.accounts[t.From()], s.accounts[t.To()]
	from.transfers = append(from.transfers, t.ID())
	to.transfers = append(to.transfers, t.ID())
}

func (s *Store) markReversed(id transfer.ID) {
	record, found := s.transfers[id]
	if !found {
		return
	}
	stored := record.transfer
	record.transfer = transfer.Restore(transfer.Snapshot{
		ID: stored.ID(),
		Request: transfer.Request{
			OwnerID:    stored.OwnerID(),
			Key:        stored.IdempotencyKey(),
			Kind:       stored.Kind(),
			From:       stored.From(),
			To:         stored.To(),
			Amount:     stored.Amount(),
			ReversalOf: stored.ReversalOf(),
		},
		Status:    transfer.StatusReversed,
		CreatedAt: stored.CreatedAt(),
	})
}

func scopeOf(t transfer.Transfer) idempotencyScope {
	return idempotencyScope{owner: t.OwnerID(), key: t.IdempotencyKey()}
}

func transferIDText(t transfer.Transfer) string {
	return string(t.ID())
}
