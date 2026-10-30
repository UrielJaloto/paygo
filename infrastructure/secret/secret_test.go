package secret_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"testing"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/infrastructure/secret"
)

var errNoEntropy = errors.New("no entropy")

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errNoEntropy
}

func sequentialBytes(count int) []byte {
	material := make([]byte, count)
	for index := range material {
		material[index] = byte(index)
	}
	return material
}

func TestNewToken_FixedBytes_ReturnsExactToken(t *testing.T) {
	material := sequentialBytes(36)
	want := "pgk_00010203_" + base64.RawURLEncoding.EncodeToString(material[4:])

	token, err := secret.NewTokenGenerator(bytes.NewReader(material)).NewToken()

	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}
	if token.String() != want {
		t.Fatalf("token = %q, want %q", token.String(), want)
	}
}

func TestNewToken_RandomBytes_Parses(t *testing.T) {
	token, err := secret.NewTokenGenerator(rand.Reader).NewToken()
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}
	if _, err := apikey.ParseToken(token.String()); err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
}

func TestNewToken_ReaderFails_WrapsError(t *testing.T) {
	_, err := secret.NewTokenGenerator(failingReader{}).NewToken()
	if !errors.Is(err, errNoEntropy) {
		t.Fatalf("error = %v, want %v", err, errNoEntropy)
	}
}

func TestNewSecret_RandomBytes_MatchesFormat(t *testing.T) {
	value, err := secret.NewTokenGenerator(rand.Reader).NewSecret()
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	if !regexp.MustCompile(`^whsec_[A-Za-z0-9_-]{43}$`).MatchString(value) {
		t.Fatalf("secret %q does not match the format", value)
	}
}

func TestNewSecret_ReaderFails_WrapsError(t *testing.T) {
	_, err := secret.NewTokenGenerator(failingReader{}).NewSecret()
	if !errors.Is(err, errNoEntropy) {
		t.Fatalf("error = %v, want %v", err, errNoEntropy)
	}
}

func TestHash_KnownVector_ReturnsDigest(t *testing.T) {
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := (secret.TokenHasher{}).Hash("abc"); got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
}

func TestMatches_Candidates_ComparesHash(t *testing.T) {
	hasher := secret.TokenHasher{}
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "same token", token: "abc", want: true},
		{name: "other token", token: "abd", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasher.Matches(test.token, hasher.Hash("abc")); got != test.want {
				t.Fatalf("Matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIsAdmin_Candidates_ComparesToken(t *testing.T) {
	checker := secret.NewAdminTokenChecker("admin-token")
	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{name: "exact", candidate: "admin-token", want: true},
		{name: "wrong", candidate: "admin-tokem", want: false},
		{name: "empty", candidate: "", want: false},
		{name: "longer", candidate: "admin-token-extra", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := checker.IsAdmin(test.candidate); got != test.want {
				t.Fatalf("IsAdmin() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSign_KnownVector_ReturnsVersionedHMAC(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	mac.Write([]byte(`1700000000.{"a":1}`))
	want := "v1=" + hex.EncodeToString(mac.Sum(nil))

	got := secret.WebhookSigner{}.Sign("whsec_test", 1700000000, []byte(`{"a":1}`))

	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}
