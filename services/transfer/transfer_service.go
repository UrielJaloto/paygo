package transfer

import (
	"context"
	"fmt"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/transfer"
)

type UnitOfWork interface {
	Run(ctx context.Context, work func(ctx context.Context, store Store) error) error
}

type Store struct {
	Accounts  AccountLocker
	Ledger    Ledger
	Transfers Repository
	Outbox    Outbox
}

type AccountLocker interface {
	LockByIDs(ctx context.Context, ids []account.ID) ([]account.Account, error)
}

type Ledger interface {
	CalculateBalance(ctx context.Context, id account.ID) (money.Money, error)
	Append(ctx context.Context, journal ledger.JournalTransaction) error
}

type Repository interface {
	FindByIdempotencyKey(ctx context.Context, owner apikey.OwnerID, key transfer.IdempotencyKey) (transfer.Transfer, transfer.Fingerprint, error)
	Save(ctx context.Context, t transfer.Transfer, fingerprint transfer.Fingerprint) error
	MarkReversed(ctx context.Context, t transfer.Transfer) error
}

type Outbox interface {
	Add(ctx context.Context, e event.Event) error
}

type Reader interface {
	FindByID(ctx context.Context, id transfer.ID) (transfer.Transfer, error)
	ListByAccount(ctx context.Context, id account.ID, cursor page.Cursor, limit page.Limit) ([]transfer.Transfer, error)
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}

type Service struct {
	unitOfWork    UnitOfWork
	reader Reader
	clock  Clock
	ids    IDGenerator
}

func NewMoney(unitOfWork UnitOfWork, reader Reader, clock Clock, ids IDGenerator) *Service {
	return &Service{unitOfWork: unitOfWork, reader: reader, clock: clock, ids: ids}
}

func (s *Service) Transfer(ctx context.Context, input TransferInput) (TransferOutput, error) {
	request, err := parseTransfer(input)
	if err != nil {
		return TransferOutput{}, err
	}
	return s.execute(ctx, request)
}

func (s *Service) Deposit(ctx context.Context, input FundingInput) (TransferOutput, error) {
	request, err := parseFunding(input)
	if err != nil {
		return TransferOutput{}, err
	}
	request.Kind = transfer.KindDeposit
	return s.execute(ctx, request)
}

func (s *Service) Withdraw(ctx context.Context, input FundingInput) (TransferOutput, error) {
	request, err := parseFunding(input)
	if err != nil {
		return TransferOutput{}, err
	}
	request.Kind = transfer.KindWithdrawal
	request.From, request.To = request.To, request.From
	return s.execute(ctx, request)
}

func (s *Service) execute(ctx context.Context, request transfer.Request) (TransferOutput, error) {
	var output TransferOutput
	err := s.unitOfWork.Run(ctx, func(ctx context.Context, store Store) error {
		posted, err := s.postRequest(ctx, store, request)
		output = posted
		return err
	})
	if err != nil {
		return TransferOutput{}, fmt.Errorf("posting transfer: %w", err)
	}
	return output, nil
}

func parseTransfer(input TransferInput) (transfer.Request, error) {
	owner, err := apikey.ParseOwnerID(input.OwnerID)
	if err != nil {
		return transfer.Request{}, err
	}
	key, err := transfer.ParseIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return transfer.Request{}, err
	}
	from, err := account.ParseID(input.FromAccountID)
	if err != nil {
		return transfer.Request{}, err
	}
	to, err := account.ParseID(input.ToAccountID)
	if err != nil {
		return transfer.Request{}, err
	}
	if from == to {
		return transfer.Request{}, fmt.Errorf("%w: %s", transfer.ErrSameAccount, from)
	}
	amount, err := parseAmount(input.Amount, input.Currency)
	if err != nil {
		return transfer.Request{}, err
	}
	return transfer.Request{OwnerID: owner, Key: key, Kind: transfer.KindTransfer, From: from, To: to, Amount: amount}, nil
}

func parseFunding(input FundingInput) (transfer.Request, error) {
	if _, err := apikey.ParseOwnerID(input.OwnerID); err != nil {
		return transfer.Request{}, err
	}
	key, err := transfer.ParseIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return transfer.Request{}, err
	}
	customer, err := account.ParseID(input.AccountID)
	if err != nil {
		return transfer.Request{}, err
	}
	amount, err := parseAmount(input.Amount, input.Currency)
	if err != nil {
		return transfer.Request{}, err
	}
	settlement := settlementAccountFor(amount.Currency())
	if customer == settlement {
		return transfer.Request{}, fmt.Errorf("%w: %s", account.ErrAccountNotFound, customer)
	}
	return transfer.Request{OwnerID: apikey.SystemOwnerID, Key: key, From: settlement, To: customer, Amount: amount}, nil
}

func parseAmount(amount, currencyCode string) (money.Money, error) {
	currency, err := money.ParseCurrency(currencyCode)
	if err != nil {
		return money.Money{}, err
	}
	return money.ParseAmount(amount, currency)
}
