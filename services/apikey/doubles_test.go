package apikey_test

import (
	"context"
	"strings"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
)

type fakeRepository struct {
	keys      map[apikey.ID]apikey.APIKey
	saved     []apikey.APIKey
	updated   []apikey.APIKey
	findErr   error
	saveErr   error
	updateErr error
}

func newFakeRepository(keys ...apikey.APIKey) *fakeRepository {
	repository := &fakeRepository{keys: map[apikey.ID]apikey.APIKey{}}
	for _, key := range keys {
		repository.keys[key.ID()] = key
	}
	return repository
}

func (f *fakeRepository) Save(_ context.Context, key apikey.APIKey) error {
	f.saved = append(f.saved, key)
	return f.saveErr
}

func (f *fakeRepository) FindByPrefix(_ context.Context, prefix apikey.Prefix) (apikey.APIKey, error) {
	if f.findErr != nil {
		return apikey.APIKey{}, f.findErr
	}
	for _, key := range f.keys {
		if key.Prefix() == prefix {
			return key, nil
		}
	}
	return apikey.APIKey{}, apikey.ErrAPIKeyNotFound
}

func (f *fakeRepository) FindByID(_ context.Context, id apikey.ID) (apikey.APIKey, error) {
	if f.findErr != nil {
		return apikey.APIKey{}, f.findErr
	}
	key, ok := f.keys[id]
	if !ok {
		return apikey.APIKey{}, apikey.ErrAPIKeyNotFound
	}
	return key, nil
}

func (f *fakeRepository) Update(_ context.Context, key apikey.APIKey) error {
	f.updated = append(f.updated, key)
	return f.updateErr
}

type stubGenerator struct {
	token apikey.Token
	err   error
}

func (s stubGenerator) NewToken() (apikey.Token, error) {
	return s.token, s.err
}

type fakeHasher struct {
	hash string
}

func (f fakeHasher) Hash(string) string {
	return f.hash
}

func (f fakeHasher) Matches(token string, hash string) bool {
	return token == validToken && hash == f.hash
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

type stubIDs struct{}

func (stubIDs) NewID() string {
	return keyID
}

var (
	validSecret = strings.Repeat("s", 43)
	validHash   = strings.Repeat("a", 64)
	validToken  = "pgk_abcdef01_" + validSecret
)
