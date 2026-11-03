package secret

import (
	"crypto/sha256"
	"crypto/subtle"
)

type AdminTokenChecker struct {
	expectedHash [sha256.Size]byte
}

func NewAdminTokenChecker(token string) AdminTokenChecker {
	return AdminTokenChecker{expectedHash: sha256.Sum256([]byte(token))}
}

func (c AdminTokenChecker) IsAdmin(candidate string) bool {
	digest := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(digest[:], c.expectedHash[:]) == 1
}
