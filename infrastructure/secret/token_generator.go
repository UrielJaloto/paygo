package secret

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/UrielJaloto/paygo/domain/apikey"
)

const (
	prefixBytes         = 4
	secretBytes         = 32
	webhookSecretPrefix = "whsec_"
)

type TokenGenerator struct {
	random io.Reader
}

func NewTokenGenerator(random io.Reader) TokenGenerator {
	return TokenGenerator{random: random}
}

func (g TokenGenerator) NewToken() (apikey.Token, error) {
	material := make([]byte, prefixBytes+secretBytes)
	if _, err := io.ReadFull(g.random, material); err != nil {
		return apikey.Token{}, fmt.Errorf("generating api token: %w", err)
	}
	prefix := hex.EncodeToString(material[:prefixBytes])
	return apikey.NewToken(prefix, base64.RawURLEncoding.EncodeToString(material[prefixBytes:]))
}

func (g TokenGenerator) NewSecret() (string, error) {
	material := make([]byte, secretBytes)
	if _, err := io.ReadFull(g.random, material); err != nil {
		return "", fmt.Errorf("generating webhook secret: %w", err)
	}
	return webhookSecretPrefix + base64.RawURLEncoding.EncodeToString(material), nil
}
