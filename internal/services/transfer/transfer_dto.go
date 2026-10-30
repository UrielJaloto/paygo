package transfer

import "time"

type TransferInput struct {
	OwnerID        string
	IdempotencyKey string
	FromAccountID  string
	ToAccountID    string
	Amount         string
	Currency       string
}

type FundingInput struct {
	OwnerID        string
	IdempotencyKey string
	AccountID      string
	Amount         string
	Currency       string
}

type ReverseInput struct {
	OwnerID        string
	IdempotencyKey string
	TransferID     string
}

type TransferQuery struct {
	OwnerID    string
	TransferID string
}

type ListInput struct {
	OwnerID   string
	AccountID string
	Cursor    string
	Limit     string
}

type TransferPage struct {
	Items      []TransferOutput
	NextCursor string
}

type TransferOutput struct {
	ID            string
	Kind          string
	Status        string
	FromAccountID string
	ToAccountID   string
	AmountMinor   int64
	Currency      string
	ReversalOf    string
	CreatedAt     time.Time
	IsReplay      bool
}
