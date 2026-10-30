package webhookhttp

import (
	"time"

	"github.com/UrielJaloto/paygo/domain/event"
)

type eventPayload struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt string          `json:"created_at"`
	Data      transferPayload `json:"data"`
}

type transferPayload struct {
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

func toPayload(source event.Event) eventPayload {
	transfer := source.Transfer()
	return eventPayload{
		ID:        string(source.ID()),
		Type:      string(source.Type()),
		CreatedAt: source.OccurredAt().UTC().Format(time.RFC3339),
		Data: transferPayload{
			ID:            string(transfer.ID()),
			Kind:          string(transfer.Kind()),
			Status:        string(transfer.Status()),
			FromAccountID: string(transfer.From()),
			ToAccountID:   string(transfer.To()),
			Amount:        transfer.Amount().Decimal(),
			Currency:      string(transfer.Amount().Currency()),
			ReversalOf:    string(transfer.ReversalOf()),
			CreatedAt:     transfer.CreatedAt().UTC().Format(time.RFC3339),
		},
	}
}
