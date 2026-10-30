package rest

import transferservice "github.com/UrielJaloto/paygo/services/transfer"

type transferResponse struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	ReversalOf    string `json:"reversal_of,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type transferPageResponse struct {
	Items      []transferResponse `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type TransferPresenter struct{}

func (TransferPresenter) transfer(output transferservice.TransferOutput) transferResponse {
	return transferResponse{
		ID:            output.ID,
		Kind:          output.Kind,
		Status:        output.Status,
		FromAccountID: output.FromAccountID,
		ToAccountID:   output.ToAccountID,
		Amount:        formatAmount(output.AmountMinor, output.Currency),
		Currency:      output.Currency,
		ReversalOf:    output.ReversalOf,
		CreatedAt:     formatTimestamp(output.CreatedAt),
	}
}

func (p TransferPresenter) transferPage(page transferservice.TransferPage) transferPageResponse {
	items := make([]transferResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, p.transfer(item))
	}
	return transferPageResponse{Items: items, NextCursor: page.NextCursor}
}
