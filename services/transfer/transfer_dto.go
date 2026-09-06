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
