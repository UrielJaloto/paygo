package payment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	cpfLength      = 11
	cnpjLength     = 14
	cpfMaxWeight   = 11
	cnpjMaxWeight  = 9
	checkDigits    = 2
	firstWeight    = 2
	checkDigitBase = 11
)

var (
	documentSeparators = strings.NewReplacer(".", "", "-", "", "/", "")
	documentPattern    = regexp.MustCompile(`^(\d{11}|\d{14})$`)
)

type Document string

func ParseDocument(text string) (Document, error) {
	digits := documentSeparators.Replace(text)
	if !documentPattern.MatchString(digits) || isRepeated(digits) || !hasValidCheckDigits(digits) {
		return "", fmt.Errorf("%w: %q", ErrInvalidDocument, text)
	}
	return Document(digits), nil
}

func isRepeated(digits string) bool {
	return strings.Count(digits, digits[:1]) == len(digits)
}

func hasValidCheckDigits(digits string) bool {
	maxWeight := cpfMaxWeight
	if len(digits) == cnpjLength {
		maxWeight = cnpjMaxWeight
	}
	body := digits[:len(digits)-checkDigits]
	first := checkDigit(body, maxWeight)
	return digits[len(body):] == first+checkDigit(body+first, maxWeight)
}

func checkDigit(digits string, maxWeight int) string {
	sum, weight := 0, firstWeight
	for i := len(digits) - 1; i >= 0; i-- {
		sum += int(digits[i]-'0') * weight
		weight++
		if weight > maxWeight {
			weight = firstWeight
		}
	}
	remainder := sum % checkDigitBase
	if remainder < firstWeight {
		return "0"
	}
	return strconv.Itoa(checkDigitBase - remainder)
}
