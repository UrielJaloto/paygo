package webhook

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
)

const (
	MaximumActiveEndpoints = 10
	MaximumURLLength       = 2048
)

var (
	ErrInvalidEndpointURL      = errors.New("invalid endpoint url")
	ErrEndpointLimitReached    = errors.New("endpoint limit reached")
	ErrEndpointNotFound        = errors.New("endpoint not found")
	ErrEndpointAlreadyDisabled = errors.New("endpoint already disabled")
	ErrInvalidSecret           = errors.New("invalid secret")
)

var secretPattern = regexp.MustCompile(`^whsec_[A-Za-z0-9_-]{43}$`)

type EndpointURL string

func ParseEndpointURL(text string) (EndpointURL, error) {
	if len(text) > MaximumURLLength {
		return "", fmt.Errorf("%w: longer than %d", ErrInvalidEndpointURL, MaximumURLLength)
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidEndpointURL, err)
	}
	if !isAllowedTarget(parsed) {
		return "", fmt.Errorf("%w: %q", ErrInvalidEndpointURL, text)
	}
	return EndpointURL(text), nil
}

func isAllowedTarget(target *url.URL) bool {
	host := target.Hostname()
	if target.Scheme == "https" {
		return host != ""
	}
	return target.Scheme == "http" && (host == "localhost" || host == "127.0.0.1")
}

type EndpointSnapshot struct {
	ID        string
	OwnerID   apikey.OwnerID
	URL       EndpointURL
	Secret    string
	IsActive  bool
	CreatedAt time.Time
}

type Endpoint struct {
	id        string
	ownerID   apikey.OwnerID
	url       EndpointURL
	secret    string
	isActive  bool
	createdAt time.Time
}

func NewEndpoint(id string, owner apikey.OwnerID, target EndpointURL, secret string, createdAt time.Time) (Endpoint, error) {
	if !secretPattern.MatchString(secret) {
		return Endpoint{}, ErrInvalidSecret
	}
	return RestoreEndpoint(EndpointSnapshot{
		ID:        id,
		OwnerID:   owner,
		URL:       target,
		Secret:    secret,
		IsActive:  true,
		CreatedAt: createdAt,
	}), nil
}

func RestoreEndpoint(snapshot EndpointSnapshot) Endpoint {
	return Endpoint{
		id:        snapshot.ID,
		ownerID:   snapshot.OwnerID,
		url:       snapshot.URL,
		secret:    snapshot.Secret,
		isActive:  snapshot.IsActive,
		createdAt: snapshot.CreatedAt,
	}
}

func (e Endpoint) ID() string {
	return e.id
}

func (e Endpoint) OwnerID() apikey.OwnerID {
	return e.ownerID
}

func (e Endpoint) URL() EndpointURL {
	return e.url
}

func (e Endpoint) Secret() string {
	return e.secret
}

func (e Endpoint) IsActive() bool {
	return e.isActive
}

func (e Endpoint) CreatedAt() time.Time {
	return e.createdAt
}

func (e *Endpoint) Disable() error {
	if !e.isActive {
		return ErrEndpointAlreadyDisabled
	}
	e.isActive = false
	return nil
}
