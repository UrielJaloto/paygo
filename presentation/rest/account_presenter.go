package rest

import (
	"time"

	"github.com/UrielJaloto/paygo/domain/money"
	accountservice "github.com/UrielJaloto/paygo/services/account"
)

const timestampLayout = "2006-01-02T15:04:05.000Z07:00"

type accountResponse struct {
	ID        string `json:"id"`
	OwnerID   string `json:"owner_id"`
	Currency  string `json:"currency"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type accountPageResponse struct {
	Items      []accountResponse `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type balanceResponse struct {
	AccountID string `json:"account_id"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
}

type statementLineResponse struct {
	EntryID    string `json:"entry_id"`
	TransferID string `json:"transfer_id"`
	Direction  string `json:"direction"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	CreatedAt  string `json:"created_at"`
}

type statementPageResponse struct {
	Items      []statementLineResponse `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type AccountPresenter struct{}

func (AccountPresenter) account(output accountservice.AccountOutput) accountResponse {
	return accountResponse{
		ID:        output.ID,
		OwnerID:   output.OwnerID,
		Currency:  output.Currency,
		Kind:      output.Kind,
		Status:    output.Status,
		CreatedAt: formatTimestamp(output.CreatedAt),
	}
}

func (p AccountPresenter) accountPage(page accountservice.AccountPage) accountPageResponse {
	items := make([]accountResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, p.account(item))
	}
	return accountPageResponse{Items: items, NextCursor: page.NextCursor}
}

func (AccountPresenter) balance(output accountservice.BalanceOutput) balanceResponse {
	return balanceResponse{
		AccountID: output.AccountID,
		Amount:    formatAmount(output.AmountMinor, output.Currency),
		Currency:  output.Currency,
	}
}

func (AccountPresenter) statementPage(page accountservice.StatementPage) statementPageResponse {
	items := make([]statementLineResponse, 0, len(page.Items))
	for _, line := range page.Items {
		items = append(items, statementLineResponse{
			EntryID:    line.EntryID,
			TransferID: line.TransferID,
			Direction:  line.Direction,
			Amount:     formatAmount(line.AmountMinor, line.Currency),
			Currency:   line.Currency,
			CreatedAt:  formatTimestamp(line.CreatedAt),
		})
	}
	return statementPageResponse{Items: items, NextCursor: page.NextCursor}
}

func formatAmount(minor int64, currency string) string {
	return money.New(minor, money.Currency(currency)).Decimal()
}

func formatTimestamp(at time.Time) string {
	return at.UTC().Format(timestampLayout)
}
