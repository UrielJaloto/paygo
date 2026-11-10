package metrics

import (
	"context"
	"errors"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	domaintransfer "github.com/UrielJaloto/paygo/internal/domain/transfer"
	"github.com/UrielJaloto/paygo/internal/services/transfer"
)

type transferService interface {
	Transfer(ctx context.Context, input transfer.TransferInput) (transfer.TransferOutput, error)
	Deposit(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error)
	Withdraw(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error)
	Reverse(ctx context.Context, input transfer.ReverseInput) (transfer.TransferOutput, error)
	Find(ctx context.Context, query transfer.TransferQuery) (transfer.TransferOutput, error)
	List(ctx context.Context, input transfer.ListInput) (transfer.TransferPage, error)
}

type TransferServiceMetrics struct {
	next     transferService
	registry *Registry
}

func NewTransferServiceMetrics(next transferService, registry *Registry) *TransferServiceMetrics {
	return &TransferServiceMetrics{next: next, registry: registry}
}

func (m *TransferServiceMetrics) Transfer(ctx context.Context, input transfer.TransferInput) (transfer.TransferOutput, error) {
	output, err := m.next.Transfer(ctx, input)
	m.countTransfer("transfer", output, err)
	return output, err
}

func (m *TransferServiceMetrics) Deposit(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error) {
	output, err := m.next.Deposit(ctx, input)
	m.countTransfer("deposit", output, err)
	return output, err
}

func (m *TransferServiceMetrics) Withdraw(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error) {
	output, err := m.next.Withdraw(ctx, input)
	m.countTransfer("withdrawal", output, err)
	return output, err
}

func (m *TransferServiceMetrics) Reverse(ctx context.Context, input transfer.ReverseInput) (transfer.TransferOutput, error) {
	output, err := m.next.Reverse(ctx, input)
	m.countTransfer("reversal", output, err)
	return output, err
}

func (m *TransferServiceMetrics) Find(ctx context.Context, query transfer.TransferQuery) (transfer.TransferOutput, error) {
	return m.next.Find(ctx, query)
}

func (m *TransferServiceMetrics) List(ctx context.Context, input transfer.ListInput) (transfer.TransferPage, error) {
	return m.next.List(ctx, input)
}

func (m *TransferServiceMetrics) countTransfer(kind string, output transfer.TransferOutput, err error) {
	m.registry.transfers.WithLabelValues(kind, resolveTransferResult(output, err)).Inc()
}

func resolveTransferResult(output transfer.TransferOutput, err error) string {
	if err != nil {
		return classifyFailure(err)
	}
	if output.IsReplay {
		return "replay"
	}
	return "success"
}

func classifyFailure(err error) string {
	for _, rejection := range listTransferRejections() {
		if errors.Is(err, rejection) {
			return "rejected"
		}
	}
	return "error"
}

func listTransferRejections() []error {
	return []error{
		account.ErrInvalidAccountID, account.ErrAccountNotFound, account.ErrAccountFrozen, account.ErrConcurrentModification,
		ledger.ErrTooFewEntries, ledger.ErrUnbalancedTransaction,
		money.ErrUnsupportedCurrency, money.ErrInvalidAmount, money.ErrAmountTooLarge,
		money.ErrCurrencyMismatch, money.ErrAmountOverflow,
		page.ErrInvalidPageLimit, page.ErrInvalidCursor,
		domaintransfer.ErrSameAccount, domaintransfer.ErrInsufficientFunds, domaintransfer.ErrTransferNotFound,
		domaintransfer.ErrTransferAlreadyReversed, domaintransfer.ErrReversalNotReversible,
		domaintransfer.ErrIdempotencyConflict, domaintransfer.ErrInvalidIdempotencyKey,
	}
}
