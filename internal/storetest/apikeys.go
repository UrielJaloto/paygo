package storetest

import (
	"context"
	"testing"

	"github.com/UrielJaloto/paygo/domain/apikey"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
)

const (
	keyPrefix apikey.Prefix = "abcdefgh"
	keyHash                 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func RunAPIKeyContract(t *testing.T, newRepository func(t *testing.T) apikeyservice.Repository) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, repository apikeyservice.Repository)
	}{
		{"save then find by id and prefix round trips", apiKeyRoundTrip},
		{"find missing is not found", apiKeyMissing},
		{"update stores the revocation", apiKeyRevoke},
		{"update of a missing key changes nothing", apiKeyUpdateMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newRepository(t))
		})
	}
}

func newAPIKey(t *testing.T) apikey.APIKey {
	t.Helper()
	created, err := apikey.New(apikey.ID(ID(1)), OwnerA, keyPrefix, keyHash, At)
	NoError(t, err)
	return created
}

func apiKeyRoundTrip(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	saved := newAPIKey(t)
	NoError(t, repository.Save(ctx, saved))
	byID, err := repository.FindByID(ctx, saved.ID())
	NoError(t, err)
	Equal(t, byID.Prefix(), keyPrefix)
	Equal(t, byID.OwnerID(), OwnerA)
	Equal(t, byID.Hash(), keyHash)
	Equal(t, byID.IsActive(), true)
	byPrefix, err := repository.FindByPrefix(ctx, keyPrefix)
	NoError(t, err)
	Equal(t, byPrefix.ID(), saved.ID())
}

func apiKeyMissing(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	_, err := repository.FindByID(ctx, apikey.ID(ID(9)))
	ErrorIs(t, err, apikey.ErrAPIKeyNotFound)
	_, err = repository.FindByPrefix(ctx, keyPrefix)
	ErrorIs(t, err, apikey.ErrAPIKeyNotFound)
}

func apiKeyRevoke(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	saved := newAPIKey(t)
	NoError(t, repository.Save(ctx, saved))
	NoError(t, saved.Revoke(At))
	NoError(t, repository.Update(ctx, saved))
	found, err := repository.FindByID(ctx, saved.ID())
	NoError(t, err)
	Equal(t, found.IsActive(), false)
	Equal(t, found.RevokedAt().Equal(At), true)
}

func apiKeyUpdateMissing(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	missing := newAPIKey(t)
	NoError(t, repository.Update(ctx, missing))
	_, err := repository.FindByID(ctx, missing.ID())
	ErrorIs(t, err, apikey.ErrAPIKeyNotFound)
}
