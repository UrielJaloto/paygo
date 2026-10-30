package secret

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

type TokenHasher struct{}

func (TokenHasher) Hash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (h TokenHasher) Matches(token, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(h.Hash(token)), []byte(hash)) == 1
}
