package account_test

import (
	"errors"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/money"
)

const (
	validID    account.ID     = "01890a5d-ac96-774b-bcce-b302099a8057"
	owner      apikey.OwnerID = "01890a5d-ac96-774b-bcce-b302099a8058"
	otherOwner apikey.OwnerID = "01890a5d-ac96-774b-bcce-b302099a8059"
)

var createdAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func newCustomer(t *testing.T) account.Account {
	t.Helper()
	created, err := account.NewCustomer(validID, owner, money.BRL, createdAt)
	if err != nil {
		t.Fatalf("NewCustomer: %v", err)
	}
	return created
}

func TestParseID_ReturnsIDOrErrInvalidAccountID(t *testing.T) {
	id, err := account.ParseID(string(validID))
	if err != nil || id != validID {
		t.Fatalf("got (%q, %v), want %s", id, err, validID)
	}
	if _, err := account.ParseID("x"); !errors.Is(err, account.ErrInvalidAccountID) {
		t.Fatalf("error = %v, want ErrInvalidAccountID", err)
	}
}

func TestNewCustomer_Valid_IsActiveAtVersionOne(t *testing.T) {
	created := newCustomer(t)
	if created.ID() != validID || created.OwnerID() != owner || created.Currency() != money.BRL {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.Kind() != account.KindCustomer || created.Status() != account.StatusActive {
		t.Fatalf("unexpected state: %+v", created)
	}
	if created.Version() != 1 || !created.CreatedAt().Equal(createdAt) {
		t.Fatalf("unexpected version or time: %+v", created)
	}
}

func TestNewCustomer_SystemOwner_ReturnsErrInvalidOwner(t *testing.T) {
	_, err := account.NewCustomer(validID, apikey.SystemOwnerID, money.BRL, createdAt)
	if !errors.Is(err, apikey.ErrInvalidOwner) {
		t.Fatalf("error = %v, want ErrInvalidOwner", err)
	}
}

func TestFreeze_Active_IncrementsVersion(t *testing.T) {
	target := newCustomer(t)
	if err := target.Freeze(); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if target.Status() != account.StatusFrozen || target.Version() != 2 {
		t.Fatalf("got %s at version %d", target.Status(), target.Version())
	}
}

func TestFreeze_Frozen_KeepsVersion(t *testing.T) {
	target := newCustomer(t)
	_ = target.Freeze()
	if err := target.Freeze(); !errors.Is(err, account.ErrAccountAlreadyFrozen) {
		t.Fatalf("error = %v, want ErrAccountAlreadyFrozen", err)
	}
	if target.Version() != 2 {
		t.Fatalf("version = %d, want 2", target.Version())
	}
}

func TestUnfreeze_Frozen_IncrementsVersion(t *testing.T) {
	target := newCustomer(t)
	_ = target.Freeze()
	if err := target.Unfreeze(); err != nil {
		t.Fatalf("Unfreeze: %v", err)
	}
	if target.Status() != account.StatusActive || target.Version() != 3 {
		t.Fatalf("got %s at version %d", target.Status(), target.Version())
	}
}

func TestUnfreeze_Active_ReturnsErrAccountNotFrozen(t *testing.T) {
	target := newCustomer(t)
	if err := target.Unfreeze(); !errors.Is(err, account.ErrAccountNotFrozen) {
		t.Fatalf("error = %v, want ErrAccountNotFrozen", err)
	}
	if target.Version() != 1 {
		t.Fatalf("version = %d, want 1", target.Version())
	}
}

func TestCanMoveMoney_FollowsStatus(t *testing.T) {
	target := newCustomer(t)
	if !target.CanMoveMoney() {
		t.Fatal("active account must move money")
	}
	_ = target.Freeze()
	if target.CanMoveMoney() {
		t.Fatal("frozen account must not move money")
	}
}

func TestAllowsNegativeBalance_FollowsKind(t *testing.T) {
	cases := []struct {
		kind account.Kind
		want bool
	}{
		{kind: account.KindCustomer, want: false},
		{kind: account.KindSettlement, want: true},
		{kind: account.KindProviderClearing, want: true},
		{kind: account.KindPayoutHold, want: false},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.kind), func(t *testing.T) {
			got := account.Restore(account.Snapshot{Kind: testCase.kind}).AllowsNegativeBalance()
			if got != testCase.want {
				t.Fatalf("AllowsNegativeBalance = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestIsOwnedBy_ComparesOwner(t *testing.T) {
	target := newCustomer(t)
	if !target.IsOwnedBy(owner) || target.IsOwnedBy(otherOwner) {
		t.Fatal("ownership must match the account owner only")
	}
}

func TestRestore_KeepsEveryField(t *testing.T) {
	snapshot := account.Snapshot{
		ID:        validID,
		OwnerID:   apikey.SystemOwnerID,
		Currency:  money.BRL,
		Kind:      account.KindSettlement,
		Status:    account.StatusFrozen,
		Version:   7,
		CreatedAt: createdAt,
	}
	restored := account.Restore(snapshot)
	if restored.ID() != validID || restored.OwnerID() != apikey.SystemOwnerID || restored.Version() != 7 {
		t.Fatalf("got %+v", restored)
	}
	if restored.Kind() != account.KindSettlement || restored.Status() != account.StatusFrozen {
		t.Fatalf("got %+v", restored)
	}
}
