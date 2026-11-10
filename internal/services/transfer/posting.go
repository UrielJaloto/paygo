package transfer

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

func (s *Service) postRequest(ctx context.Context, store Store, request transfer.Request) (TransferOutput, error) {
	replayed, found, err := findReplay(ctx, store, request)
	if err != nil || found {
		return replayed, err
	}
	return s.moveFunds(ctx, store, request)
}

func findReplay(ctx context.Context, store Store, request transfer.Request) (TransferOutput, bool, error) {
	existing, fingerprint, err := store.Transfers.FindByIdempotencyKey(ctx, request.OwnerID, request.Key)
	if errors.Is(err, transfer.ErrTransferNotFound) {
		return TransferOutput{}, false, nil
	}
	if err != nil {
		return TransferOutput{}, false, fmt.Errorf("finding idempotency key: %w", err)
	}
	if fingerprint != request.CalculateFingerprint() {
		return TransferOutput{}, false, fmt.Errorf("%w: %s", transfer.ErrIdempotencyConflict, request.Key)
	}
	output := toOutput(existing)
	output.IsReplay = true
	return output, true, nil
}

func (s *Service) moveFunds(ctx context.Context, store Store, request transfer.Request) (TransferOutput, error) {
	from, to, err := lockAccountPair(ctx, store, request)
	if err != nil {
		return TransferOutput{}, err
	}
	replayed, found, err := findReplay(ctx, store, request)
	if err != nil || found {
		return replayed, err
	}
	if err := validateAccounts(request, from, to); err != nil {
		return TransferOutput{}, err
	}
	if err := validateFunds(ctx, store, request, from); err != nil {
		return TransferOutput{}, err
	}
	return s.recordTransfer(ctx, store, request, []apikey.OwnerID{from.OwnerID(), to.OwnerID()})
}

func lockAccountPair(ctx context.Context, store Store, request transfer.Request) (account.Account, account.Account, error) {
	ids := []account.ID{request.From, request.To}
	slices.Sort(ids)
	locked, err := store.Accounts.LockByIDs(ctx, ids)
	if err != nil {
		return account.Account{}, account.Account{}, fmt.Errorf("locking accounts: %w", err)
	}
	if locked[0].ID() == request.From {
		return locked[0], locked[1], nil
	}
	return locked[1], locked[0], nil
}

func validateAccounts(request transfer.Request, from, to account.Account) error {
	if !canMoveFunds(request, from, to) {
		return fmt.Errorf("%w: %s", account.ErrAccountNotFound, request.From)
	}
	if from.Currency() != request.Amount.Currency() || to.Currency() != request.Amount.Currency() {
		return fmt.Errorf("%w: accounts and %s", money.ErrCurrencyMismatch, request.Amount.Currency())
	}
	if !from.CanMoveMoney() || !to.CanMoveMoney() {
		return fmt.Errorf("%w: %s to %s", account.ErrAccountFrozen, request.From, request.To)
	}
	return nil
}

func canMoveFunds(request transfer.Request, from, to account.Account) bool {
	switch request.Kind {
	case transfer.KindTransfer:
		return from.IsOwnedBy(request.OwnerID)
	case transfer.KindDeposit:
		return to.Kind() == account.KindCustomer
	case transfer.KindWithdrawal:
		return from.Kind() == account.KindCustomer
	default:
		return true
	}
}

func validateFunds(ctx context.Context, store Store, request transfer.Request, from account.Account) error {
	if from.AllowsNegativeBalance() {
		return nil
	}
	balance, err := store.Ledger.FindBalance(ctx, from.ID())
	if err != nil {
		return fmt.Errorf("reading balance: %w", err)
	}
	remaining, err := balance.Subtract(request.Amount)
	if err != nil {
		return fmt.Errorf("checking funds: %w", err)
	}
	if remaining.IsNegative() {
		return fmt.Errorf("%w: account %s", transfer.ErrInsufficientFunds, from.ID())
	}
	return nil
}

func (s *Service) recordTransfer(ctx context.Context, store Store, request transfer.Request, owners []apikey.OwnerID) (TransferOutput, error) {
	created, err := transfer.NewTransfer(s.ids.NewID(), request, s.now())
	if err != nil {
		return TransferOutput{}, err
	}
	journal, err := created.BuildJournal([2]string{s.ids.NewID(), s.ids.NewID()})
	if err != nil {
		return TransferOutput{}, err
	}
	if err := store.Ledger.Append(ctx, journal); err != nil {
		return TransferOutput{}, fmt.Errorf("appending journal: %w", err)
	}
	if err := store.Transfers.Save(ctx, created, request.CalculateFingerprint()); err != nil {
		return TransferOutput{}, fmt.Errorf("saving transfer: %w", err)
	}
	metadata := event.Metadata{ID: s.ids.NewID(), At: created.CreatedAt()}
	if err := store.Outbox.Add(ctx, event.NewTransferEvent(metadata, created, owners)); err != nil {
		return TransferOutput{}, fmt.Errorf("adding event: %w", err)
	}
	return toOutput(created), nil
}

func toOutput(transfer transfer.Transfer) TransferOutput {
	return TransferOutput{
		ID:            string(transfer.ID()),
		Kind:          string(transfer.Kind()),
		Status:        string(transfer.Status()),
		FromAccountID: string(transfer.From()),
		ToAccountID:   string(transfer.To()),
		AmountMinor:   transfer.Amount().Minor(),
		Currency:      string(transfer.Amount().Currency()),
		ReversalOf:    string(transfer.ReversalOf()),
		CreatedAt:     transfer.CreatedAt(),
	}
}
