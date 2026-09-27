package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/transfer"
)

type transferService interface {
	Transfer(ctx context.Context, input transfer.TransferInput) (transfer.TransferOutput, error)
	Deposit(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error)
	Withdraw(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error)
	Reverse(ctx context.Context, input transfer.ReverseInput) (transfer.TransferOutput, error)
	Get(ctx context.Context, query transfer.TransferQuery) (transfer.TransferOutput, error)
	List(ctx context.Context, input transfer.ListInput) (transfer.TransferPage, error)
}

type TransferServiceLogger struct {
	next   transferService
	logger *slog.Logger
}

func NewTransferServiceLogger(next transferService, logger *slog.Logger) *TransferServiceLogger {
	return &TransferServiceLogger{next: next, logger: logger}
}

func (l *TransferServiceLogger) Transfer(ctx context.Context, input transfer.TransferInput) (transfer.TransferOutput, error) {
	op := begin(l.logger, "transfer.Transfer")
	output, err := l.next.Transfer(ctx, input)
	op.finish(ctx, err,
		slog.String("owner_id", input.OwnerID),
		slog.String("from_account_id", input.FromAccountID),
		slog.String("to_account_id", input.ToAccountID),
		slog.String("amount", input.Amount))
	return output, err
}

func (l *TransferServiceLogger) Deposit(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error) {
	op := begin(l.logger, "transfer.Deposit")
	output, err := l.next.Deposit(ctx, input)
	op.finish(ctx, err, fundingAttrs(input)...)
	return output, err
}

func (l *TransferServiceLogger) Withdraw(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error) {
	op := begin(l.logger, "transfer.Withdraw")
	output, err := l.next.Withdraw(ctx, input)
	op.finish(ctx, err, fundingAttrs(input)...)
	return output, err
}

func (l *TransferServiceLogger) Reverse(ctx context.Context, input transfer.ReverseInput) (transfer.TransferOutput, error) {
	op := begin(l.logger, "transfer.Reverse")
	output, err := l.next.Reverse(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID), slog.String("transfer_id", input.TransferID))
	return output, err
}

func (l *TransferServiceLogger) Get(ctx context.Context, query transfer.TransferQuery) (transfer.TransferOutput, error) {
	op := begin(l.logger, "transfer.Get")
	output, err := l.next.Get(ctx, query)
	op.finish(ctx, err, slog.String("owner_id", query.OwnerID), slog.String("transfer_id", query.TransferID))
	return output, err
}

func (l *TransferServiceLogger) List(ctx context.Context, input transfer.ListInput) (transfer.TransferPage, error) {
	op := begin(l.logger, "transfer.List")
	output, err := l.next.List(ctx, input)
	op.finish(ctx, err, slog.String("owner_id", input.OwnerID), slog.String("account_id", input.AccountID))
	return output, err
}

func fundingAttrs(input transfer.FundingInput) []slog.Attr {
	return []slog.Attr{
		slog.String("owner_id", input.OwnerID),
		slog.String("account_id", input.AccountID),
		slog.String("amount", input.Amount),
	}
}
