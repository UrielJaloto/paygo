package payment

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	PixKeyCPF   PixKeyType = "cpf"
	PixKeyCNPJ  PixKeyType = "cnpj"
	PixKeyEmail PixKeyType = "email"
	PixKeyPhone PixKeyType = "phone"
	PixKeyEVP   PixKeyType = "evp"

	maxEmailLength = 77
)

var (
	phonePattern = regexp.MustCompile(`^\+55\d{10,11}$`)
	evpPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

type PixKeyType string

type PixKey struct {
	kind  PixKeyType
	value string
}

func ParsePixKey(kind, value string) (PixKey, error) {
	keyType := PixKeyType(kind)
	normalized, valid := normalizePixValue(keyType, value)
	if !valid {
		return PixKey{}, fmt.Errorf("%w: %s %q", ErrInvalidPixKey, kind, value)
	}
	return PixKey{kind: keyType, value: normalized}, nil
}

func normalizePixValue(kind PixKeyType, value string) (string, bool) {
	switch kind {
	case PixKeyCPF:
		return documentOfLength(value, cpfLength)
	case PixKeyCNPJ:
		return documentOfLength(value, cnpjLength)
	case PixKeyEmail:
		return value, isValidEmail(value)
	case PixKeyPhone:
		return value, phonePattern.MatchString(value)
	case PixKeyEVP:
		return value, evpPattern.MatchString(value)
	}
	return "", false
}

func documentOfLength(value string, length int) (string, bool) {
	document, err := ParseDocument(value)
	return string(document), err == nil && len(document) == length
}

func isValidEmail(value string) bool {
	_, domain, found := strings.Cut(value, "@")
	return found && len(value) <= maxEmailLength && !strings.Contains(domain, "@") && strings.Contains(domain, ".")
}

func (k PixKey) Kind() PixKeyType {
	return k.kind
}

func (k PixKey) Value() string {
	return k.value
}
