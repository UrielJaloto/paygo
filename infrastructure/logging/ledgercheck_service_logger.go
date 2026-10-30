package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/ledgercheck"
)

type ledgerCheckService interface {
	Check(ctx context.Context) (ledgercheck.CheckOutput, error)
}

type LedgerCheckServiceLogger struct {
	next   ledgerCheckService
	logger *slog.Logger
}

func NewLedgerCheckServiceLogger(next ledgerCheckService, logger *slog.Logger) *LedgerCheckServiceLogger {
	return &LedgerCheckServiceLogger{next: next, logger: logger}
}

func (l *LedgerCheckServiceLogger) Check(ctx context.Context) (ledgercheck.CheckOutput, error) {
	op := begin(l.logger, "ledgercheck.Check")
	output, err := l.next.Check(ctx)
	op.finish(ctx, err, slog.Bool("ok", output.OK), slog.Int("accounts", output.Accounts), slog.Int("mismatches", len(output.Mismatches)))
	return output, err
}
