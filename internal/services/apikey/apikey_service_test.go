package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	service "github.com/UrielJaloto/paygo/internal/services/apikey"
)

const (
	keyID = "key-1"
	owner = "0190c000-0000-7000-8000-000000000001"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func storedKey(t *testing.T) apikey.APIKey {
	t.Helper()
	key, err := apikey.New(keyID, owner, "abcdef01", validHash, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return key
}

func newService(t *testing.T, repository *fakeRepository) *service.Service {
	t.Helper()
	token, err := apikey.ParseToken(validToken)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	return service.New(repository, stubGenerator{token: token}, fakeHasher{hash: validHash}, stubClock{now: now}, stubIDs{})
}

func TestIssue_Valid_ReturnsTokenAndStoresOnlyHash(t *testing.T) {
	repository := newFakeRepository()
	got, err := newService(t, repository).Issue(context.Background(), service.IssueInput{OwnerID: owner})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	want := service.IssuedKey{ID: keyID, OwnerID: owner, Token: validToken, CreatedAt: now}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(repository.saved) != 1 || repository.saved[0].Hash() != validHash {
		t.Fatalf("saved = %+v", repository.saved)
	}
}

func TestIssue_InvalidOwner_ReturnsInvalidOwner(t *testing.T) {
	for _, text := range []string{"x", string(apikey.SystemOwnerID)} {
		t.Run(text, func(t *testing.T) {
			repository := newFakeRepository()
			_, err := newService(t, repository).Issue(context.Background(), service.IssueInput{OwnerID: text})
			if !errors.Is(err, apikey.ErrInvalidOwner) || len(repository.saved) != 0 {
				t.Fatalf("error = %v, saved = %d", err, len(repository.saved))
			}
		})
	}
}

func TestIssue_GeneratorFails_DoesNotSave(t *testing.T) {
	repository := newFakeRepository()
	svc := service.New(repository, stubGenerator{err: errBoom}, fakeHasher{hash: validHash}, stubClock{now: now}, stubIDs{})
	_, err := svc.Issue(context.Background(), service.IssueInput{OwnerID: owner})
	if !errors.Is(err, errBoom) || len(repository.saved) != 0 {
		t.Fatalf("error = %v, saved = %d", err, len(repository.saved))
	}
}

func TestIssue_InvalidHash_DoesNotSave(t *testing.T) {
	repository := newFakeRepository()
	token, err := apikey.ParseToken(validToken)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	svc := service.New(repository, stubGenerator{token: token}, fakeHasher{hash: "bad"}, stubClock{now: now}, stubIDs{})
	_, err = svc.Issue(context.Background(), service.IssueInput{OwnerID: owner})
	if !errors.Is(err, apikey.ErrInvalidToken) || len(repository.saved) != 0 {
		t.Fatalf("error = %v, saved = %d", err, len(repository.saved))
	}
}

func TestIssue_SaveFails_ReturnsWrappedError(t *testing.T) {
	repository := newFakeRepository()
	repository.saveErr = errBoom
	_, err := newService(t, repository).Issue(context.Background(), service.IssueInput{OwnerID: owner})
	if !errors.Is(err, errBoom) || strings.Contains(err.Error(), validSecret) {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthenticate_Valid_ReturnsPrincipal(t *testing.T) {
	got, err := newService(t, newFakeRepository(storedKey(t))).Authenticate(context.Background(), validToken)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.OwnerID != owner {
		t.Fatalf("owner = %s", got.OwnerID)
	}
}

func TestAuthenticate_Rejected_ReturnsUnauthenticated(t *testing.T) {
	revoked := storedKey(t)
	if err := revoked.Revoke(now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	cases := []struct {
		name       string
		token      string
		repository *fakeRepository
	}{
		{name: "malformed", token: "nope", repository: newFakeRepository(storedKey(t))},
		{name: "unknown", token: validToken, repository: newFakeRepository()},
		{name: "mismatch", token: "pgk_abcdef01_" + strings.Repeat("x", 43), repository: newFakeRepository(storedKey(t))},
		{name: "revoked", token: validToken, repository: newFakeRepository(revoked)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newService(t, tc.repository).Authenticate(context.Background(), tc.token)
			if !errors.Is(err, service.ErrUnauthenticated) || strings.Contains(err.Error(), tc.token) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAuthenticate_RepositoryFails_ReturnsWrappedError(t *testing.T) {
	repository := newFakeRepository()
	repository.findErr = errBoom
	_, err := newService(t, repository).Authenticate(context.Background(), validToken)
	if !errors.Is(err, errBoom) || errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("error = %v", err)
	}
}

func TestRevoke_Valid_UpdatesRevokedKey(t *testing.T) {
	repository := newFakeRepository(storedKey(t))
	if err := newService(t, repository).Revoke(context.Background(), service.RevokeInput{KeyID: keyID}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if len(repository.updated) != 1 || !repository.updated[0].RevokedAt().Equal(now) {
		t.Fatalf("updated = %+v", repository.updated)
	}
}

func TestRevoke_Failures_ReturnExactError(t *testing.T) {
	revoked := storedKey(t)
	if err := revoked.Revoke(now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	failingUpdate := newFakeRepository(storedKey(t))
	failingUpdate.updateErr = errBoom
	cases := []struct {
		name       string
		repository *fakeRepository
		want       error
	}{
		{name: "missing", repository: newFakeRepository(), want: apikey.ErrAPIKeyNotFound},
		{name: "twice", repository: newFakeRepository(revoked), want: apikey.ErrAPIKeyAlreadyRevoked},
		{name: "update fails", repository: failingUpdate, want: errBoom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newService(t, tc.repository).Revoke(context.Background(), service.RevokeInput{KeyID: keyID})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
