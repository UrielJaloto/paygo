package page

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

const (
	DefaultLimit Limit = 20

	minimumLimit = 1
	maximumLimit = 100
)

var (
	ErrInvalidPageLimit = errors.New("invalid page limit")
	ErrInvalidCursor    = errors.New("invalid cursor")
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type Limit int

func NewLimit(value int) (Limit, error) {
	if value < minimumLimit || value > maximumLimit {
		return 0, fmt.Errorf("%w: %d", ErrInvalidPageLimit, value)
	}
	return Limit(value), nil
}

func ParseLimit(text string) (Limit, error) {
	if text == "" {
		return DefaultLimit, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidPageLimit, text)
	}
	return NewLimit(value)
}

type Cursor struct {
	afterID string
}

func NewCursor(afterID string) Cursor {
	return Cursor{afterID: afterID}
}

func ParseCursor(text string) (Cursor, error) {
	if text == "" {
		return Cursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if !idPattern.Match(decoded) {
		return Cursor{}, fmt.Errorf("%w: not an id", ErrInvalidCursor)
	}
	return NewCursor(string(decoded)), nil
}

func (c Cursor) Encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.afterID))
}

func (c Cursor) AfterID() string {
	return c.afterID
}

func (c Cursor) IsStart() bool {
	return c.afterID == ""
}
