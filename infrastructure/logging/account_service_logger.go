package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/account"
)

type accountService interface {
	Open(ctx context.Context, input account.OpenInput) (account.AccountOutput, error)
	Get(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error)
	List(ctx context.Context, input account.ListInput) (account.AccountPage, error)
	CalculateBalance(ctx context.Context, query account.AccountQuery) (account.BalanceOutput, error)
	ListStatement(ctx context.Context, input account.PageInput) (account.StatementPage, error)
	Freeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error)
	Unfreeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error)
}

type AccountServiceLogger struct {
	next   accountService
	logger *slog.Logger
}

func NewAccountServiceLogger(next accountService, logger *slog.Logger) *AccountServiceLogger {
	return &AccountServiceLogger{next: next, logger: logger}
}

func (l *AccountServiceLogger) Open(ctx context.Context, input account.OpenInput) (account.AccountOutput, error) {
	op := begin(l.logger, "account.Open")
	output, err := l.next.Open(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID))
	return output, err
}

func (l *AccountServiceLogger) Get(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error) {
	op := begin(l.logger, "account.Get")
	output, err := l.next.Get(ctx, query)
	op.finish(ctx, err, accountQueryAttrs(query)...)
	return output, err
}

func (l *AccountServiceLogger) List(ctx context.Context, input account.ListInput) (account.AccountPage, error) {
	op := begin(l.logger, "account.List")
	output, err := l.next.List(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID))
	return output, err
}

func (l *AccountServiceLogger) CalculateBalance(ctx context.Context, query account.AccountQuery) (account.BalanceOutput, error) {
	op := begin(l.logger, "account.CalculateBalance")
	output, err := l.next.CalculateBalance(ctx, query)
	op.finish(ctx, err, accountQueryAttrs(query)...)
	return output, err
}

func (l *AccountServiceLogger) ListStatement(ctx context.Context, input account.PageInput) (account.StatementPage, error) {
	op := begin(l.logger, "account.Statement")
	output, err := l.next.ListStatement(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID), slog.String("account_id", input.AccountID))
	return output, err
}

func (l *AccountServiceLogger) Freeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error) {
	op := begin(l.logger, "account.Freeze")
	output, err := l.next.Freeze(ctx, query)
	op.finish(ctx, err, accountQueryAttrs(query)...)
	return output, err
}

func (l *AccountServiceLogger) Unfreeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error) {
	op := begin(l.logger, "account.Unfreeze")
	output, err := l.next.Unfreeze(ctx, query)
	op.finish(ctx, err, accountQueryAttrs(query)...)
	return output, err
}

func accountQueryAttrs(query account.AccountQuery) []slog.Attr {
	return []slog.Attr{slog.String("owner_id", query.OwnerID), slog.String("account_id", query.AccountID)}
}
