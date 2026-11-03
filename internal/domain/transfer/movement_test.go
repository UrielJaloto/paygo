package transfer_test

import (
	"errors"
	"testing"

	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

func TestPaymentConstructors_SetKindAndValidate(t *testing.T) {
	cases := []struct {
		name  string
		build func(transfer.ID, transfer.Movement) (transfer.Transfer, error)
		want  transfer.Kind
	}{
		{name: "charge", want: transfer.KindCharge, build: func(id transfer.ID, m transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewCharge(id, m, createdAt)
		}},
		{name: "payout", want: transfer.KindPayout, build: func(id transfer.ID, m transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewPayout(id, m, createdAt)
		}},
		{name: "completion", want: transfer.KindPayoutCompletion, build: func(id transfer.ID, m transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewPayoutCompletion(id, m, createdAt)
		}},
		{name: "failure", want: transfer.KindPayoutFailure, build: func(id transfer.ID, m transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewPayoutFailure(id, m, createdAt)
		}},
	}
	movement := transfer.Movement{OwnerID: owner, From: accountA, To: accountB, Amount: money.New(100, money.BRL)}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			created, err := tc.build("t1", movement)
			if err != nil || created.Kind() != tc.want || created.Status() != transfer.StatusCompleted {
				t.Fatalf("got (%+v, %v)", created, err)
			}
			same := movement
			same.To = same.From
			if _, err := tc.build("t1", same); !errors.Is(err, transfer.ErrSameAccount) {
				t.Fatalf("error = %v, want ErrSameAccount", err)
			}
		})
	}
}
