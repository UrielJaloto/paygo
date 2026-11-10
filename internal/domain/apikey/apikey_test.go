package apikey_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
)

const (
	validPrefix = "0a1b2c3d"
	validOwner  = "01890a5d-ac96-774b-bcce-b302099a8057"
)

var (
	validSecret = strings.Repeat("a", 43)
	validHash   = strings.Repeat("f", 64)
	createdAt   = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func TestParseToken_Valid_ReturnsPrefix(t *testing.T) {
	token, err := apikey.ParseToken("pgk_" + validPrefix + "_" + validSecret)
	if err != nil || token.Prefix() != validPrefix {
		t.Fatalf("got (%q, %v), want prefix %s", token.Prefix(), err, validPrefix)
	}
}

func TestParseToken_Malformed_ReturnsErrInvalidToken(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "wrong scheme", text: "pk_" + validPrefix + "_" + validSecret},
		{name: "prefix uppercase", text: "pgk_0A1B2C3D_" + validSecret},
		{name: "secret 42 chars", text: "pgk_" + validPrefix + "_" + validSecret[:42]},
		{name: "secret 44 chars", text: "pgk_" + validPrefix + "_" + validSecret + "a"},
		{name: "extra part", text: "pgk_x_y_z"},
		{name: "empty", text: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := apikey.ParseToken(testCase.text)
			if !errors.Is(err, apikey.ErrInvalidToken) || got != (apikey.Token{}) {
				t.Fatalf("got (%+v, %v), want ErrInvalidToken", got, err)
			}
		})
	}
}

func TestNewToken_RoundTrip_KeepsValue(t *testing.T) {
	token, err := apikey.NewToken(validPrefix, validSecret)
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	parsed, err := apikey.ParseToken(token.String())
	if err != nil || parsed != token {
		t.Fatalf("got (%+v, %v), want %+v", parsed, err, token)
	}
}

func TestNewToken_Invalid_ReturnsErrInvalidToken(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		secret string
	}{
		{name: "bad prefix", prefix: "XYZ", secret: validSecret},
		{name: "bad secret", prefix: validPrefix, secret: "short"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := apikey.NewToken(testCase.prefix, testCase.secret); !errors.Is(err, apikey.ErrInvalidToken) {
				t.Fatalf("error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestParseOwnerID_ReturnsOwnerOrErrInvalidOwner(t *testing.T) {
	owner, err := apikey.ParseOwnerID(validOwner)
	if err != nil || owner != validOwner {
		t.Fatalf("got (%q, %v), want %s", owner, err, validOwner)
	}
	if _, err := apikey.ParseOwnerID("not-uuid"); !errors.Is(err, apikey.ErrInvalidOwner) {
		t.Fatalf("error = %v, want ErrInvalidOwner", err)
	}
}

func TestNewAPIKey_ValidHash_ExposesFields(t *testing.T) {
	key, err := apikey.NewAPIKey("key-1", validOwner, validPrefix, validHash, createdAt)
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}
	if key.ID() != "key-1" || key.OwnerID() != validOwner || key.Prefix() != validPrefix {
		t.Fatalf("unexpected identity: %+v", key)
	}
	if key.Hash() != validHash || !key.CreatedAt().Equal(createdAt) || !key.IsActive() {
		t.Fatalf("unexpected state: %+v", key)
	}
}

func TestNewAPIKey_InvalidHash_ReturnsErrInvalidToken(t *testing.T) {
	if _, err := apikey.NewAPIKey("key-1", validOwner, validPrefix, "xyz", createdAt); !errors.Is(err, apikey.ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}

func TestRevoke_Active_RecordsTime(t *testing.T) {
	key, _ := apikey.NewAPIKey("key-1", validOwner, validPrefix, validHash, createdAt)
	revokedAt := createdAt.Add(time.Hour)
	if err := key.Revoke(revokedAt); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if key.IsActive() || !key.RevokedAt().Equal(revokedAt) {
		t.Fatalf("active %v revoked at %v", key.IsActive(), key.RevokedAt())
	}
}

func TestRevoke_Twice_ReturnsErrAPIKeyAlreadyRevoked(t *testing.T) {
	key, _ := apikey.NewAPIKey("key-1", validOwner, validPrefix, validHash, createdAt)
	first := createdAt.Add(time.Hour)
	_ = key.Revoke(first)
	if err := key.Revoke(first.Add(time.Hour)); !errors.Is(err, apikey.ErrAPIKeyAlreadyRevoked) {
		t.Fatalf("error = %v, want ErrAPIKeyAlreadyRevoked", err)
	}
	if !key.RevokedAt().Equal(first) {
		t.Fatalf("revoked at changed to %v", key.RevokedAt())
	}
}

func TestRestore_KeepsRevocation(t *testing.T) {
	key, _ := apikey.NewAPIKey("key-1", validOwner, validPrefix, validHash, createdAt)
	restored := apikey.Restore(key, createdAt)
	if restored.IsActive() || !restored.RevokedAt().Equal(createdAt) {
		t.Fatalf("got %+v", restored)
	}
}
