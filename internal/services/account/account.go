package account

import (
	"context"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/page"
)

type Repository interface {
	Save(ctx context.Context, account account.Account) error
	FindByID(ctx context.Context, id account.ID) (account.Account, error)
	ListByOwner(ctx context.Context, owner apikey.OwnerID, cursor page.Cursor, limit page.Limit) ([]account.Account, error)
	Update(ctx context.Context, account account.Account, expectedVersion int64) error
}

type LedgerReader interface {
	FindBalance(ctx context.Context, id account.ID) (money.Money, error)
	ListStatement(ctx context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]ledger.Entry, error)
}

type IDGenerator interface {
	NewID() string
}

type Service struct {
	repository Repository
	ledger     LedgerReader
	now        func() time.Time
	ids        IDGenerator
}

func NewService(repository Repository, ledger LedgerReader, now func() time.Time, ids IDGenerator) *Service {
	return &Service{repository: repository, ledger: ledger, now: now, ids: ids}
}

func (s *Service) Open(ctx context.Context, input OpenInput) (AccountOutput, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return AccountOutput{}, err
	}
	currency, err := money.ParseCurrency(input.Currency)
	if err != nil {
		return AccountOutput{}, err
	}
	opened, err := account.NewCustomer(account.ID(s.ids.NewID()), owner, currency, s.now())
	if err != nil {
		return AccountOutput{}, err
	}
	if err := s.repository.Save(ctx, opened); err != nil {
		return AccountOutput{}, fmt.Errorf("opening account: %w", err)
	}
	return toOutput(opened), nil
}

func (s *Service) Find(ctx context.Context, query AccountQuery) (AccountOutput, error) {
	found, err := s.findOwnedAccount(ctx, query)
	if err != nil {
		return AccountOutput{}, err
	}
	return toOutput(found), nil
}

func (s *Service) List(ctx context.Context, input ListInput) (AccountPage, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return AccountPage{}, err
	}
	cursor, limit, err := parsePage(input.Cursor, input.Limit)
	if err != nil {
		return AccountPage{}, err
	}
	accounts, err := s.repository.ListByOwner(ctx, owner, cursor, limit+1)
	if err != nil {
		return AccountPage{}, fmt.Errorf("listing accounts: %w", err)
	}
	items, next := trimPage(accounts, limit, func(account account.Account) string { return string(account.ID()) })
	output := AccountPage{Items: make([]AccountOutput, 0, len(items)), NextCursor: next}
	for _, item := range items {
		output.Items = append(output.Items, toOutput(item))
	}
	return output, nil
}

func (s *Service) FindBalance(ctx context.Context, query AccountQuery) (BalanceOutput, error) {
	found, err := s.findOwnedAccount(ctx, query)
	if err != nil {
		return BalanceOutput{}, err
	}
	balance, err := s.ledger.FindBalance(ctx, found.ID())
	if err != nil {
		return BalanceOutput{}, fmt.Errorf("reading balance: %w", err)
	}
	return BalanceOutput{
		AccountID:   string(found.ID()),
		AmountMinor: balance.Minor(),
		Currency:    string(balance.Currency()),
	}, nil
}

func (s *Service) ListStatement(ctx context.Context, input PageInput) (StatementPage, error) {
	cursor, limit, err := parsePage(input.Cursor, input.Limit)
	if err != nil {
		return StatementPage{}, err
	}
	found, err := s.findOwnedAccount(ctx, AccountQuery{OwnerID: input.OwnerID, AccountID: input.AccountID})
	if err != nil {
		return StatementPage{}, err
	}
	entries, err := s.ledger.ListStatement(ctx, found.ID(), cursor, limit+1)
	if err != nil {
		return StatementPage{}, fmt.Errorf("reading statement: %w", err)
	}
	items, next := trimPage(entries, limit, ledger.Entry.ID)
	output := StatementPage{Items: make([]StatementLine, 0, len(items)), NextCursor: next}
	for _, item := range items {
		output.Items = append(output.Items, toLine(item))
	}
	return output, nil
}

func (s *Service) Freeze(ctx context.Context, query AccountQuery) (AccountOutput, error) {
	return s.changeStatus(ctx, query, (*account.Account).Freeze)
}

func (s *Service) Unfreeze(ctx context.Context, query AccountQuery) (AccountOutput, error) {
	return s.changeStatus(ctx, query, (*account.Account).Unfreeze)
}

func (s *Service) changeStatus(ctx context.Context, query AccountQuery, change func(*account.Account) error) (AccountOutput, error) {
	found, err := s.findOwnedAccount(ctx, query)
	if err != nil {
		return AccountOutput{}, err
	}
	previousVersion := found.Version()
	if err := change(&found); err != nil {
		return AccountOutput{}, err
	}
	if err := s.repository.Update(ctx, found, previousVersion); err != nil {
		return AccountOutput{}, fmt.Errorf("updating account: %w", err)
	}
	return toOutput(found), nil
}

func (s *Service) findOwnedAccount(ctx context.Context, query AccountQuery) (account.Account, error) {
	owner, err := apikey.ParseOwnerID(query.OwnerID)
	if err != nil {
		return account.Account{}, err
	}
	id, err := account.ParseID(query.AccountID)
	if err != nil {
		return account.Account{}, err
	}
	found, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return account.Account{}, fmt.Errorf("finding account: %w", err)
	}
	if !found.IsOwnedBy(owner) {
		return account.Account{}, fmt.Errorf("%w: %s", account.ErrAccountNotFound, id)
	}
	return found, nil
}

func parsePage(cursorText, limitText string) (page.Cursor, page.Limit, error) {
	cursor, err := page.ParseCursor(cursorText)
	if err != nil {
		return page.Cursor{}, 0, err
	}
	limit, err := page.ParseLimit(limitText)
	if err != nil {
		return page.Cursor{}, 0, err
	}
	return cursor, limit, nil
}

func trimPage[T any](items []T, limit page.Limit, idOf func(T) string) ([]T, string) {
	if len(items) <= int(limit) {
		return items, ""
	}
	kept := items[:limit]
	return kept, page.NewCursor(idOf(kept[len(kept)-1])).Encode()
}

func toOutput(account account.Account) AccountOutput {
	return AccountOutput{
		ID:        string(account.ID()),
		OwnerID:   string(account.OwnerID()),
		Currency:  string(account.Currency()),
		Kind:      string(account.Kind()),
		Status:    string(account.Status()),
		Version:   account.Version(),
		CreatedAt: account.CreatedAt(),
	}
}

func toLine(entry ledger.Entry) StatementLine {
	return StatementLine{
		EntryID:     entry.ID(),
		TransferID:  entry.JournalID(),
		Direction:   string(entry.Direction()),
		AmountMinor: entry.Amount().Minor(),
		Currency:    string(entry.Amount().Currency()),
		CreatedAt:   entry.CreatedAt(),
	}
}
