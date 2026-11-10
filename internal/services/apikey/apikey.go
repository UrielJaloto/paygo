package apikey

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Repository interface {
	Save(ctx context.Context, key apikey.APIKey) error
	FindByPrefix(ctx context.Context, prefix string) (apikey.APIKey, error)
	FindByID(ctx context.Context, id string) (apikey.APIKey, error)
	Update(ctx context.Context, key apikey.APIKey) error
}

type TokenGenerator interface {
	NewToken() (apikey.Token, error)
}

type IDGenerator interface {
	NewID() string
}

type Service struct {
	repository Repository
	tokens     TokenGenerator
	hashToken  func(token string) string
	now        func() time.Time
	ids        IDGenerator
}

func NewService(repository Repository, tokens TokenGenerator, hashToken func(token string) string, now func() time.Time, ids IDGenerator) *Service {
	return &Service{repository: repository, tokens: tokens, hashToken: hashToken, now: now, ids: ids}
}

func (s *Service) Issue(ctx context.Context, input IssueInput) (IssuedKey, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return IssuedKey{}, err
	}
	if owner == apikey.SystemOwnerID {
		return IssuedKey{}, fmt.Errorf("%w: system owner", apikey.ErrInvalidOwner)
	}
	token, err := s.tokens.NewToken()
	if err != nil {
		return IssuedKey{}, fmt.Errorf("generating token: %w", err)
	}
	key, err := apikey.NewAPIKey(s.ids.NewID(), owner, token.Prefix(), s.hashToken(token.String()), s.now())
	if err != nil {
		return IssuedKey{}, err
	}
	if err := s.repository.Save(ctx, key); err != nil {
		return IssuedKey{}, fmt.Errorf("saving api key: %w", err)
	}
	return IssuedKey{ID: string(key.ID()), OwnerID: string(owner), Token: token.String(), CreatedAt: key.CreatedAt()}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	parsed, err := apikey.ParseToken(token)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	key, err := s.repository.FindByPrefix(ctx, parsed.Prefix())
	if errors.Is(err, apikey.ErrAPIKeyNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("finding api key: %w", err)
	}
	if !s.matchesHash(token, key.Hash()) || !key.IsActive() {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{OwnerID: string(key.OwnerID())}, nil
}

func (s *Service) Revoke(ctx context.Context, input RevokeInput) error {
	key, err := s.repository.FindByID(ctx, input.KeyID)
	if err != nil {
		return fmt.Errorf("finding api key: %w", err)
	}
	if err := key.Revoke(s.now()); err != nil {
		return err
	}
	if err := s.repository.Update(ctx, key); err != nil {
		return fmt.Errorf("revoking api key: %w", err)
	}
	return nil
}

func (s *Service) matchesHash(token, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(s.hashToken(token)), []byte(hash)) == 1
}
