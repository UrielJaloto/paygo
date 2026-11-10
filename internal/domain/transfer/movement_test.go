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
		build func(string, transfer.Movement) (transfer.Transfer, error)
		want  string
	}{
		{name: "charge", want: transfer.KindCharge, build: func(id string, movement transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewCharge(id, movement, createdAt)
		}},
		{name: "payout", want: transfer.KindPayout, build: func(id string, movement transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewPayout(id, movement, createdAt)
		}},
		{name: "completion", want: transfer.KindPayoutCompletion, build: func(id string, movement transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewPayoutCompletion(id, movement, createdAt)
		}},
		{name: "failure", want: transfer.KindPayoutFailure, build: func(id string, movement transfer.Movement) (transfer.Transfer, error) {
			return transfer.NewPayoutFailure(id, movement, createdAt)
		}},
	}
	movement := transfer.Movement{OwnerID: owner, From: accountA, To: accountB, Amount: money.NewMoney(100, money.BRL)}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			created, err := testCase.build("t1", movement)
			if err != nil || created.Kind() != testCase.want || created.Status() != transfer.StatusCompleted {
				t.Fatalf("got (%+v, %v)", created, err)
			}
			same := movement
			same.To = same.From
			if _, err := testCase.build("t1", same); !errors.Is(err, transfer.ErrSameAccount) {
				t.Fatalf("error = %v, want ErrSameAccount", err)
			}
		})
	}
}
