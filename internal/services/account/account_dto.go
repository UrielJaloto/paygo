package account

import "time"

type OpenInput struct {
	OwnerID  string
	Currency string
}

type AccountQuery struct {
	OwnerID   string
	AccountID string
}

type ListInput struct {
	OwnerID string
	Cursor  string
	Limit   string
}

type PageInput struct {
	OwnerID   string
	AccountID string
	Cursor    string
	Limit     string
}

type AccountOutput struct {
	ID        string
	OwnerID   string
	Currency  string
	Kind      string
	Status    string
	Version   int64
	CreatedAt time.Time
}

type AccountPage struct {
	Items      []AccountOutput
	NextCursor string
}

type BalanceOutput struct {
	AccountID   string
	AmountMinor int64
	Currency    string
}

type StatementLine struct {
	EntryID     string
	TransferID  string
	Direction   string
	AmountMinor int64
	Currency    string
	CreatedAt   time.Time
}

type StatementPage struct {
	Items      []StatementLine
	NextCursor string
}
