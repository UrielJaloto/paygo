package secret

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

const signatureVersion = "v1="

type WebhookSigner struct{}

func (WebhookSigner) Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return signatureVersion + hex.EncodeToString(mac.Sum(nil))
}
