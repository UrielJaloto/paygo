package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/apikey"
)

type apikeyService interface {
	Issue(ctx context.Context, input apikey.IssueInput) (apikey.IssuedKey, error)
	Authenticate(ctx context.Context, token string) (apikey.Principal, error)
	Revoke(ctx context.Context, input apikey.RevokeInput) error
}

type APIKeyServiceLogger struct {
	next   apikeyService
	logger *slog.Logger
}

func NewAPIKeyServiceLogger(next apikeyService, logger *slog.Logger) *APIKeyServiceLogger {
	return &APIKeyServiceLogger{next: next, logger: logger}
}

func (l *APIKeyServiceLogger) Issue(ctx context.Context, input apikey.IssueInput) (apikey.IssuedKey, error) {
	op := begin(l.logger, "apikey.Issue")
	output, err := l.next.Issue(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID))
	return output, err
}

func (l *APIKeyServiceLogger) Authenticate(ctx context.Context, token string) (apikey.Principal, error) {
	op := begin(l.logger, "apikey.Authenticate")
	output, err := l.next.Authenticate(ctx, token)
	op.finish(ctx, err)
	return output, err
}

func (l *APIKeyServiceLogger) Revoke(ctx context.Context, input apikey.RevokeInput) error {
	op := begin(l.logger, "apikey.Revoke")
	err := l.next.Revoke(ctx, input)
	op.finish(ctx, err, slog.String("key_id", input.KeyID))
	return err
}
