package secret

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

const signatureVersion = "v1="

type WebhookSigner struct{}

func (WebhookSigner) SignWebhook(secret string, timestamp int64, body []byte) string {
	authenticationCode := hmac.New(sha256.New, []byte(secret))
	authenticationCode.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	authenticationCode.Write(body)
	return signatureVersion + hex.EncodeToString(authenticationCode.Sum(nil))
}
