package payment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	cpfLength         = 11
	cnpjLength        = 14
	cpfMaximumWeight  = 11
	cnpjMaximumWeight = 9
	checkDigits       = 2
	firstWeight       = 2
	checkDigitBase    = 11
)

var (
	documentSeparators = strings.NewReplacer(".", "", "-", "", "/", "")
	documentPattern    = regexp.MustCompile(`^(\d{11}|\d{14})$`)
)

type Document string

func ParseDocument(text string) (Document, error) {
	digits := documentSeparators.Replace(text)
	if !documentPattern.MatchString(digits) || hasRepeatedDigits(digits) || !hasValidCheckDigits(digits) {
		return "", fmt.Errorf("%w: %q", ErrInvalidDocument, text)
	}
	return Document(digits), nil
}

func hasRepeatedDigits(digits string) bool {
	return strings.Count(digits, digits[:1]) == len(digits)
}

func hasValidCheckDigits(digits string) bool {
	maximumWeight := cpfMaximumWeight
	if len(digits) == cnpjLength {
		maximumWeight = cnpjMaximumWeight
	}
	body := digits[:len(digits)-checkDigits]
	first := calculateCheckDigit(body, maximumWeight)
	return digits[len(body):] == first+calculateCheckDigit(body+first, maximumWeight)
}

func calculateCheckDigit(digits string, maximumWeight int) string {
	sum, weight := 0, firstWeight
	for index := len(digits) - 1; index >= 0; index-- {
		sum += int(digits[index]-'0') * weight
		weight++
		if weight > maximumWeight {
			weight = firstWeight
		}
	}
	remainder := sum % checkDigitBase
	if remainder < firstWeight {
		return "0"
	}
	return strconv.Itoa(checkDigitBase - remainder)
}
