package memory

import (
	"context"

	"github.com/UrielJaloto/paygo/domain/apikey"
)

type APIKeyRepository struct {
	store *Store
}

func NewAPIKeyRepository(store *Store) *APIKeyRepository {
	return &APIKeyRepository{store: store}
}

func (r *APIKeyRepository) Save(_ context.Context, key apikey.APIKey) error {
	r.store.data.Lock()
	defer r.store.data.Unlock()
	for _, stored := range r.store.apiKeys {
		if stored.ID() == key.ID() || stored.Prefix() == key.Prefix() {
			return ErrDuplicateID
		}
	}
	r.store.apiKeys[key.ID()] = key
	return nil
}

func (r *APIKeyRepository) FindByPrefix(_ context.Context, prefix apikey.Prefix) (apikey.APIKey, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	for _, stored := range r.store.apiKeys {
		if stored.Prefix() == prefix {
			return stored, nil
		}
	}
	return apikey.APIKey{}, apikey.ErrAPIKeyNotFound
}

func (r *APIKeyRepository) FindByID(_ context.Context, id apikey.ID) (apikey.APIKey, error) {
	r.store.data.RLock()
	defer r.store.data.RUnlock()
	stored, found := r.store.apiKeys[id]
	if !found {
		return apikey.APIKey{}, apikey.ErrAPIKeyNotFound
	}
	return stored, nil
}

func (r *APIKeyRepository) Update(_ context.Context, key apikey.APIKey) error {
	r.store.data.Lock()
	defer r.store.data.Unlock()
	if _, found := r.store.apiKeys[key.ID()]; found {
		r.store.apiKeys[key.ID()] = key
	}
	return nil
}
