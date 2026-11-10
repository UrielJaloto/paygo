package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type IdempotencyKey string

func ParseIdempotencyKey(text string) (IdempotencyKey, error) {
	if !keyPattern.MatchString(text) {
		return "", fmt.Errorf("%w: %q", ErrInvalidIdempotencyKey, text)
	}
	return IdempotencyKey(text), nil
}

func (r Request) CalculateFingerprint() string {
	canonical := fmt.Sprintf("%s|%s|%s|%d|%s", r.Kind, r.From, r.To, r.Amount.Minor(), r.Amount.Currency())
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
