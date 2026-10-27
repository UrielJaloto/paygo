//go:build integration

package postgres_test

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

func runAPIKeyContract(t *testing.T, newRepository func(t *testing.T) apikeyservice.Repository) {
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
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.run(t, newRepository(t))
		})
	}
}

func newAPIKey(t *testing.T) apikey.APIKey {
	t.Helper()
	created, err := apikey.NewMoney(apikey.ID(testID(1)), testOwner, keyPrefix, keyHash, testTime)
	noError(t, err)
	return created
}

func apiKeyRoundTrip(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	saved := newAPIKey(t)
	noError(t, repository.Save(ctx, saved))
	byID, err := repository.FindByID(ctx, saved.ID())
	noError(t, err)
	equal(t, byID.Prefix(), keyPrefix)
	equal(t, byID.OwnerID(), testOwner)
	equal(t, byID.Hash(), keyHash)
	equal(t, byID.IsActive(), true)
	byPrefix, err := repository.FindByPrefix(ctx, keyPrefix)
	noError(t, err)
	equal(t, byPrefix.ID(), saved.ID())
}

func apiKeyMissing(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	_, err := repository.FindByID(ctx, apikey.ID(testID(9)))
	errorIs(t, err, apikey.ErrAPIKeyNotFound)
	_, err = repository.FindByPrefix(ctx, keyPrefix)
	errorIs(t, err, apikey.ErrAPIKeyNotFound)
}

func apiKeyRevoke(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	saved := newAPIKey(t)
	noError(t, repository.Save(ctx, saved))
	noError(t, saved.Revoke(testTime))
	noError(t, repository.Update(ctx, saved))
	found, err := repository.FindByID(ctx, saved.ID())
	noError(t, err)
	equal(t, found.IsActive(), false)
	equal(t, found.RevokedAt().Equal(testTime), true)
}

func apiKeyUpdateMissing(t *testing.T, repository apikeyservice.Repository) {
	ctx := context.Background()
	missing := newAPIKey(t)
	noError(t, repository.Update(ctx, missing))
	_, err := repository.FindByID(ctx, missing.ID())
	errorIs(t, err, apikey.ErrAPIKeyNotFound)
}
