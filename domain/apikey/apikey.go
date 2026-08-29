package apikey

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

const SystemOwnerID OwnerID = "00000000-0000-7000-8000-000000000000"

var (
	ErrInvalidToken         = errors.New("invalid token")
	ErrInvalidOwner         = errors.New("invalid owner")
	ErrAPIKeyAlreadyRevoked = errors.New("api key already revoked")
	ErrAPIKeyNotFound       = errors.New("api key not found")
)

var (
	ownerPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	prefixPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)
	secretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	tokenPattern  = regexp.MustCompile(`^pgk_([0-9a-f]{8})_([A-Za-z0-9_-]{43})$`)
	hashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type ID string

type OwnerID string

func ParseOwnerID(text string) (OwnerID, error) {
	if !ownerPattern.MatchString(text) {
		return "", fmt.Errorf("%w: %q", ErrInvalidOwner, text)
	}
	return OwnerID(text), nil
}

type Prefix string

type Token struct {
	prefix Prefix
	secret string
}

func NewToken(prefix, secret string) (Token, error) {
	if !prefixPattern.MatchString(prefix) || !secretPattern.MatchString(secret) {
		return Token{}, ErrInvalidToken
	}
	return Token{prefix: Prefix(prefix), secret: secret}, nil
}

func ParseToken(text string) (Token, error) {
	parts := tokenPattern.FindStringSubmatch(text)
	if parts == nil {
		return Token{}, ErrInvalidToken
	}
	return Token{prefix: Prefix(parts[1]), secret: parts[2]}, nil
}

func (t Token) String() string {
	return "pgk_" + string(t.prefix) + "_" + t.secret
}

func (t Token) Prefix() Prefix {
	return t.prefix
}

type APIKey struct {
	id        ID
	ownerID   OwnerID
	prefix    Prefix
	hash      string
	createdAt time.Time
	revokedAt time.Time
}

func NewMoney(id ID, owner OwnerID, prefix Prefix, hash string, createdAt time.Time) (APIKey, error) {
	if !hashPattern.MatchString(hash) {
		return APIKey{}, ErrInvalidToken
	}
	return APIKey{id: id, ownerID: owner, prefix: prefix, hash: hash, createdAt: createdAt}, nil
}

func Restore(key APIKey, revokedAt time.Time) APIKey {
	key.revokedAt = revokedAt
	return key
}

func (k APIKey) ID() ID {
	return k.id
}

func (k APIKey) OwnerID() OwnerID {
	return k.ownerID
}

func (k APIKey) Prefix() Prefix {
	return k.prefix
}

func (k APIKey) Hash() string {
	return k.hash
}

func (k APIKey) CreatedAt() time.Time {
	return k.createdAt
}

func (k APIKey) RevokedAt() time.Time {
	return k.revokedAt
}

func (k *APIKey) Revoke(at time.Time) error {
	if !k.IsActive() {
		return ErrAPIKeyAlreadyRevoked
	}
	k.revokedAt = at
	return nil
}

func (k APIKey) IsActive() bool {
	return k.revokedAt.IsZero()
}
