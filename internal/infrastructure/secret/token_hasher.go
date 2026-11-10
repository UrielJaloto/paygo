package secret

import (
	"crypto/sha256"
	"encoding/hex"
)

func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
