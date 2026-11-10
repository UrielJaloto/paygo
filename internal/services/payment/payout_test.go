package payment_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	service "github.com/UrielJaloto/paygo/internal/services/payment"
)

func payoutInput() service.PayoutInput {
	return service.PayoutInput{
		OwnerID:        ownerID,
		IdempotencyKey: "key-1",
		AccountID:      customerID,
		Amount:         "10.00",
		Currency:       "BRL",
		PixKey:         "ana@example.com",
		PixKeyType:     "email",
	}
}

func fundedStore(t *testing.T) *fakeStore {
	t.Helper()
	store := paymentStore()
	store.balances[customerID] = brl(t, 1000)
	return store
}

func seedPayout(t *testing.T, store *fakeStore, id, providerID string) {
	t.Helper()
	store.payouts.items = append(store.payouts.items, payment.RestorePayout(payment.PayoutSnapshot{
		ID:        id,
		OwnerID:   ownerID,
		AccountID: customerID,
		Amount:    brl(t, 1000),
		Reference: payment.Reference{Provider: "fake", ProviderID: providerID},
		Transfers: payment.PayoutTransfers{Hold: "hold-1"},
		Status:    payment.PayoutPending,
		Timeline:  payment.Timeline{CreatedAt: testNow, UpdatedAt: testNow},
	}))
}

func payoutEvent(kind string, eventID string) *spyProvider {
	return &spyProvider{parsed: payment.ProviderEvent{Provider: "fake", EventID: eventID, Kind: kind, ProviderID: providerReference}}
}

func webhook() service.WebhookInput {
	return service.WebhookInput{Provider: "fake"}
}

func TestCreatePayout_ValidInput_HoldsFundsAndAttachesProvider(t *testing.T) {
	store := fundedStore(t)
	provider := &spyProvider{payoutResult: service.PayoutResult{ProviderID: providerReference}}

	got, err := newService(store, provider).CreatePayout(context.Background(), payoutInput())
	if err != nil {
		t.Fatalf("CreatePayout() error = %v", err)
	}

	want := service.PayoutOutput{ID: "id-2", AccountID: customerID, Status: "pending", PixKeyType: "email", AmountMinor: 1000, Currency: "BRL", CreatedAt: testNow}
	if got != want {
		t.Errorf("CreatePayout() = %+v, want %+v", got, want)
	}
	if len(store.transfers) != 1 || store.transfers[0].Kind() != transfer.KindPayout || store.transfers[0].To() != holdID {
		t.Errorf("transfers = %+v, want one hold", store.transfers)
	}
	if store.payouts.items[0].Reference().ProviderID != providerReference {
		t.Errorf("provider id = %q, want %q", store.payouts.items[0].Reference().ProviderID, providerReference)
	}
	if len(provider.payoutRequests) != 1 || provider.payoutRequests[0].ExternalReference != "id-2" {
		t.Errorf("requests = %+v, want reference id-2", provider.payoutRequests)
	}
}

func TestCreatePayout_InvalidInput_ReturnsErrorWithoutCallingProvider(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*service.PayoutInput)
		want   error
	}{
		{name: "owner", mutate: func(input *service.PayoutInput) { input.OwnerID = "nope" }, want: apikey.ErrInvalidOwner},
		{name: "key", mutate: func(input *service.PayoutInput) { input.IdempotencyKey = "" }, want: transfer.ErrInvalidIdempotencyKey},
		{name: "account", mutate: func(input *service.PayoutInput) { input.AccountID = "nope" }, want: account.ErrInvalidAccountID},
		{name: "currency code", mutate: func(input *service.PayoutInput) { input.Currency = "XXX" }, want: money.ErrUnsupportedCurrency},
		{name: "currency not accepted", mutate: func(input *service.PayoutInput) { input.Currency = "USD" }, want: money.ErrUnsupportedCurrency},
		{name: "amount", mutate: func(input *service.PayoutInput) { input.Amount = "abc" }, want: money.ErrInvalidAmount},
		{name: "pix key", mutate: func(input *service.PayoutInput) { input.PixKey = "" }, want: payment.ErrInvalidPixKey},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &spyProvider{}
			input := payoutInput()
			testCase.mutate(&input)

			_, err := newService(fundedStore(t), provider).CreatePayout(context.Background(), input)

			if !errors.Is(err, testCase.want) {
				t.Errorf("CreatePayout() error = %v, want %v", err, testCase.want)
			}
			if len(provider.payoutRequests) != 0 {
				t.Errorf("provider called %d times, want 0", len(provider.payoutRequests))
			}
		})
	}
}

func TestCreatePayout_AccountRefused_ReturnsErrorWithoutHold(t *testing.T) {
	frozen := account.Restore(account.Snapshot{ID: customerID, OwnerID: ownerID, Currency: money.BRL, Kind: account.KindCustomer, Status: account.StatusFrozen})
	tests := []struct {
		name    string
		account account.Account
		want    error
	}{
		{name: "other owner", account: newAccount(customerID, otherOwnerID, account.KindCustomer, money.BRL), want: account.ErrAccountNotFound},
		{name: "system account", account: newAccount(customerID, ownerID, account.KindProviderClearing, money.BRL), want: account.ErrAccountNotFound},
		{name: "other currency", account: newAccount(customerID, ownerID, account.KindCustomer, money.USD), want: account.ErrAccountNotFound},
		{name: "frozen", account: frozen, want: account.ErrAccountFrozen},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := fundedStore(t)
			store.accounts[customerID] = testCase.account

			_, err := newService(store, &spyProvider{}).CreatePayout(context.Background(), payoutInput())

			if !errors.Is(err, testCase.want) {
				t.Errorf("CreatePayout() error = %v, want %v", err, testCase.want)
			}
			if store.wasCalled("append") {
				t.Error("journal appended, want none")
			}
		})
	}
}

func TestCreatePayout_InsufficientFunds_DoesNotCallProvider(t *testing.T) {
	store := paymentStore()
	store.balances[customerID] = brl(t, 999)
	provider := &spyProvider{}

	_, err := newService(store, provider).CreatePayout(context.Background(), payoutInput())

	if !errors.Is(err, transfer.ErrInsufficientFunds) {
		t.Errorf("CreatePayout() error = %v, want %v", err, transfer.ErrInsufficientFunds)
	}
	if len(provider.payoutRequests) != 0 {
		t.Errorf("provider called %d times, want 0", len(provider.payoutRequests))
	}
}

func TestCreatePayout_BalanceInOtherCurrency_ReturnsMismatch(t *testing.T) {
	store := paymentStore()
	store.balances[customerID] = money.NewMoney(1000, money.USD)

	_, err := newService(store, &spyProvider{}).CreatePayout(context.Background(), payoutInput())

	if !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("CreatePayout() error = %v, want %v", err, money.ErrCurrencyMismatch)
	}
}

func TestCreatePayout_ProviderUnavailable_StaysPendingWithHold(t *testing.T) {
	store := fundedStore(t)
	provider := &spyProvider{payoutErr: payment.ErrProviderUnavailable}

	got, err := newService(store, provider).CreatePayout(context.Background(), payoutInput())
	if err != nil {
		t.Fatalf("CreatePayout() error = %v", err)
	}

	if got.Status != "pending" {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if store.payouts.items[0].Reference().ProviderID != "" {
		t.Errorf("provider id = %q, want empty", store.payouts.items[0].Reference().ProviderID)
	}
	if len(store.transfers) != 1 {
		t.Errorf("transfers = %d, want the hold only", len(store.transfers))
	}
}

func TestCreatePayout_ProviderRefuses_FailsAndRefunds(t *testing.T) {
	for _, cause := range []error{payment.ErrProviderRejected, payment.ErrInvalidPixKey} {
		t.Run(cause.Error(), func(t *testing.T) {
			store := fundedStore(t)

			_, err := newService(store, &spyProvider{payoutErr: cause}).CreatePayout(context.Background(), payoutInput())

			if !errors.Is(err, cause) {
				t.Errorf("CreatePayout() error = %v, want %v", err, cause)
			}
			if store.payouts.items[0].Status() != payment.PayoutFailed {
				t.Errorf("status = %q, want failed", store.payouts.items[0].Status())
			}
			if len(store.transfers) != 2 || store.transfers[1].Kind() != transfer.KindPayoutFailure || store.transfers[1].To() != customerID {
				t.Errorf("transfers = %+v, want hold and refund", store.transfers)
			}
			if len(store.events) != 1 || store.events[0].Type() != event.TypePayoutFailed {
				t.Errorf("events = %+v, want payout.failed", store.events)
			}
		})
	}
}

func TestCreatePayout_ProviderFailsUnexpectedly_ReturnsError(t *testing.T) {
	_, err := newService(fundedStore(t), &spyProvider{payoutErr: errBoom}).CreatePayout(context.Background(), payoutInput())

	if !errors.Is(err, errBoom) {
		t.Errorf("CreatePayout() error = %v, want %v", err, errBoom)
	}
}

func TestCreatePayout_RefundCannotPost_JoinsBothErrors(t *testing.T) {
	store := fundedStore(t)
	store.failures["payout.update"] = errBoom

	_, err := newService(store, &spyProvider{payoutErr: payment.ErrProviderRejected}).CreatePayout(context.Background(), payoutInput())

	if !errors.Is(err, payment.ErrProviderRejected) || !errors.Is(err, errBoom) {
		t.Errorf("CreatePayout() error = %v, want both causes", err)
	}
}

func TestCreatePayout_StoreCallFails_ReturnsError(t *testing.T) {
	for _, call := range []string{"payout.byKey", "lock", "balance", "append", "transfer.save", "payout.save", "payout.update"} {
		t.Run(call, func(t *testing.T) {
			store := fundedStore(t)
			store.failures[call] = errBoom

			_, err := newService(store, &spyProvider{}).CreatePayout(context.Background(), payoutInput())

			if !errors.Is(err, errBoom) {
				t.Errorf("CreatePayout() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestCreatePayout_SameKey_ReplaysOrConflicts(t *testing.T) {
	store := fundedStore(t)
	service := newService(store, &spyProvider{payoutResult: service.PayoutResult{ProviderID: providerReference}})
	if _, err := service.CreatePayout(context.Background(), payoutInput()); err != nil {
		t.Fatalf("CreatePayout() error = %v", err)
	}

	replayed, err := service.CreatePayout(context.Background(), payoutInput())
	if err != nil || !replayed.IsReplay {
		t.Errorf("replay = %+v, %v, want IsReplay", replayed, err)
	}

	changed := payoutInput()
	changed.Amount = "9.00"
	if _, err := service.CreatePayout(context.Background(), changed); !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Errorf("conflict error = %v, want %v", err, transfer.ErrIdempotencyConflict)
	}
}

func TestFindPayout_ReturnsOnlyOwnPayouts(t *testing.T) {
	tests := []struct {
		name  string
		query service.PayoutQuery
		want  error
	}{
		{name: "owner", query: service.PayoutQuery{OwnerID: ownerID, PayoutID: "payout-1"}},
		{name: "other owner", query: service.PayoutQuery{OwnerID: otherOwnerID, PayoutID: "payout-1"}, want: payment.ErrPayoutNotFound},
		{name: "invalid owner", query: service.PayoutQuery{OwnerID: "nope", PayoutID: "payout-1"}, want: apikey.ErrInvalidOwner},
		{name: "missing", query: service.PayoutQuery{OwnerID: ownerID, PayoutID: "missing"}, want: payment.ErrPayoutNotFound},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := paymentStore()
			seedPayout(t, store, "payout-1", providerReference)

			_, err := newService(store, &spyProvider{}).FindPayout(context.Background(), testCase.query)

			if !errors.Is(err, testCase.want) {
				t.Errorf("FindPayout() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestHandleWebhook_PayoutDone_PostsCompletionToClearing(t *testing.T) {
	store := paymentStore()
	seedPayout(t, store, "payout-1", providerReference)

	if err := newService(store, payoutEvent(payment.EventPayoutDone, "e1")).HandleWebhook(context.Background(), webhook()); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	if len(store.transfers) != 1 || store.transfers[0].Kind() != transfer.KindPayoutCompletion || store.transfers[0].To() != clearingID {
		t.Errorf("transfers = %+v, want completion to clearing", store.transfers)
	}
	if len(store.events) != 1 || store.events[0].Type() != event.TypePayoutCompleted {
		t.Errorf("events = %+v, want payout.completed", store.events)
	}
}

func TestHandleWebhook_PayoutFailedTwice_RefundsOnce(t *testing.T) {
	store := paymentStore()
	seedPayout(t, store, "payout-1", providerReference)

	for _, id := range []string{"e1", "e2"} {
		if err := newService(store, payoutEvent(payment.EventPayoutFailed, id)).HandleWebhook(context.Background(), webhook()); err != nil {
			t.Fatalf("HandleWebhook(%s) error = %v", id, err)
		}
	}

	if len(store.transfers) != 1 || store.transfers[0].To() != customerID {
		t.Errorf("transfers = %+v, want one refund", store.transfers)
	}
	if len(store.events) != 1 || store.events[0].Type() != event.TypePayoutFailed {
		t.Errorf("events = %+v, want one payout.failed", store.events)
	}
}

func TestHandleWebhook_PayoutDoneThenFailed_IgnoresSecond(t *testing.T) {
	store := paymentStore()
	seedPayout(t, store, "payout-1", providerReference)
	service := newService(store, payoutEvent(payment.EventPayoutDone, "e1"))
	if err := service.HandleWebhook(context.Background(), webhook()); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	err := newService(store, payoutEvent(payment.EventPayoutFailed, "e2")).HandleWebhook(context.Background(), webhook())

	if err != nil {
		t.Errorf("HandleWebhook() error = %v, want nil", err)
	}
	if len(store.transfers) != 1 || store.payouts.items[0].Status() != payment.PayoutCompleted {
		t.Errorf("status = %q transfers = %d, want completed and 1", store.payouts.items[0].Status(), len(store.transfers))
	}
}

func TestHandleWebhook_PayoutStoreCallFails_ReturnsError(t *testing.T) {
	for _, call := range []string{"payout.byProvider", "lock", "append", "payout.update", "outbox"} {
		t.Run(call, func(t *testing.T) {
			store := paymentStore()
			seedPayout(t, store, "payout-1", providerReference)
			store.failures[call] = errBoom

			err := newService(store, payoutEvent(payment.EventPayoutDone, "e1")).HandleWebhook(context.Background(), webhook())

			if !errors.Is(err, errBoom) {
				t.Errorf("HandleWebhook() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestSync_PayoutWithoutProviderID_RetriesWithSameReference(t *testing.T) {
	store := paymentStore()
	seedPayout(t, store, "payout-1", "")
	provider := &spyProvider{payoutResult: service.PayoutResult{ProviderID: providerReference}}

	got, err := newService(store, provider).Sync(context.Background(), 10)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if want := (service.SyncResult{Checked: 1, Retried: 1}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
	if provider.payoutRequests[0].ExternalReference != "payout-1" {
		t.Errorf("reference = %q, want payout-1", provider.payoutRequests[0].ExternalReference)
	}
}

func TestSync_ProviderReportsChanges_AppliesThem(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	seedPayout(t, store, "payout-1", "payout_ref")
	provider := &spyProvider{kinds: map[string]string{providerReference: payment.EventChargePaid, "payout_ref": payment.EventPayoutDone}}

	got, err := newService(store, provider).Sync(context.Background(), 10)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if want := (service.SyncResult{Checked: 2, Changed: 2}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
	if !store.recorded["fake/sync:fake_1:charge_paid"] {
		t.Errorf("recorded = %v, want sync event id", store.recorded)
	}
}

func TestSync_NothingNew_ChangesNothing(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	seedPayout(t, store, "payout-1", "payout_ref")
	provider := &spyProvider{kinds: map[string]string{providerReference: payment.EventIgnored, "payout_ref": payment.EventIgnored}}

	got, err := newService(store, provider).Sync(context.Background(), 10)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if want := (service.SyncResult{Checked: 2}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
}

func TestSync_ChargePastGrace_Expires(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	provider := &spyProvider{kinds: map[string]string{providerReference: payment.EventIgnored}}
	service := service.NewService(&fakeUnitOfWork{store: store}, provider, stubClock{now: testNow.Add(25*time.Hour + time.Second)}.Now, &sequenceIDs{})

	got, err := service.Sync(context.Background(), 10)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if got.Changed != 1 || store.charges.items[0].Status() != payment.ChargeExpired {
		t.Errorf("Sync() = %+v status = %q, want expired", got, store.charges.items[0].Status())
	}
}

func TestSync_ApplyingReportedChangeFails_CountsNothingAndReturnsError(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	store.failures["charge.update"] = errBoom
	provider := &spyProvider{kinds: map[string]string{providerReference: payment.EventChargePaid}}

	got, err := newService(store, provider).Sync(context.Background(), 10)

	if !errors.Is(err, errBoom) {
		t.Fatalf("Sync() error = %v, want %v", err, errBoom)
	}
	if want := (service.SyncResult{Checked: 1}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
}

func TestSync_ExpiryFails_ReturnsError(t *testing.T) {
	for _, call := range []string{"charge.find", "charge.update"} {
		t.Run(call, func(t *testing.T) {
			store := paymentStore()
			seedCharge(t, store, payment.ChargePending)
			store.failures[call] = errBoom
			provider := &spyProvider{kinds: map[string]string{providerReference: payment.EventIgnored}}
			service := service.NewService(&fakeUnitOfWork{store: store}, provider, stubClock{now: testNow.Add(26 * time.Hour)}.Now, &sequenceIDs{})

			_, err := service.Sync(context.Background(), 10)

			if !errors.Is(err, errBoom) {
				t.Errorf("Sync() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestSync_OneItemFails_ProcessesOthersAndJoinsError(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	seedPayout(t, store, "payout-1", "bad_ref")
	seedPayout(t, store, "payout-2", "good_ref")
	provider := &spyProvider{
		kinds:          map[string]string{providerReference: payment.EventIgnored, "good_ref": payment.EventPayoutDone},
		statusFailures: map[string]error{"bad_ref": errBoom},
	}

	got, err := newService(store, provider).Sync(context.Background(), 10)

	if !errors.Is(err, errBoom) {
		t.Errorf("Sync() error = %v, want %v", err, errBoom)
	}
	if want := (service.SyncResult{Checked: 3, Changed: 1}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
}

func TestSync_ChargeStatusFails_ReturnsError(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	provider := &spyProvider{statusFailures: map[string]error{providerReference: errBoom}}

	_, err := newService(store, provider).Sync(context.Background(), 10)

	if !errors.Is(err, errBoom) {
		t.Errorf("Sync() error = %v, want %v", err, errBoom)
	}
}

func TestSync_ListFails_ReturnsError(t *testing.T) {
	for _, call := range []string{"charge.pending", "payout.pending"} {
		t.Run(call, func(t *testing.T) {
			store := paymentStore()
			store.failures[call] = errBoom

			_, err := newService(store, &spyProvider{}).Sync(context.Background(), 10)

			if !errors.Is(err, errBoom) {
				t.Errorf("Sync() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestHandleWebhook_PayoutDoneTwice_PostsOnce(t *testing.T) {
	store := paymentStore()
	seedPayout(t, store, "payout-1", providerReference)

	for _, id := range []string{"e1", "e2"} {
		if err := newService(store, payoutEvent(payment.EventPayoutDone, id)).HandleWebhook(context.Background(), webhook()); err != nil {
			t.Fatalf("HandleWebhook(%s) error = %v", id, err)
		}
	}

	if len(store.transfers) != 1 || len(store.events) != 1 {
		t.Errorf("transfers = %d, events = %d, want 1 and 1", len(store.transfers), len(store.events))
	}
}

func TestSync_ChargeWithoutProviderID_SkipsProvider(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	store.charges.items[0].AttachProvider("", "", "")
	provider := &spyProvider{statusFailures: map[string]error{"": errBoom}}

	got, err := newService(store, provider).Sync(context.Background(), 10)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if want := (service.SyncResult{Checked: 1}); got != want {
		t.Errorf("Sync() = %+v, want %+v", got, want)
	}
}

func TestSync_ChargePaidAfterListing_KeepsItPaid(t *testing.T) {
	store := paymentStore()
	listed := seedCharge(t, store, payment.ChargePending)
	store.charges.stale = []payment.Charge{listed}
	if _, err := store.charges.items[0].MarkPaid("transfer-1", testNow); err != nil {
		t.Fatalf("MarkPaid() error = %v", err)
	}
	provider := &spyProvider{kinds: map[string]string{providerReference: payment.EventIgnored}}
	service := service.NewService(&fakeUnitOfWork{store: store}, provider, stubClock{now: testNow.Add(26 * time.Hour)}.Now, &sequenceIDs{})

	_, err := service.Sync(context.Background(), 10)

	if err != nil {
		t.Fatalf("Sync() error = %v, want nil", err)
	}
	if got := store.charges.items[0].Status(); got != payment.ChargePaid || len(store.events) != 0 {
		t.Errorf("status = %s, events = %d, want paid and none", got, len(store.events))
	}
}

func TestCreatePayout_SameKeyCommittedWhileWaitingForLock_Replays(t *testing.T) {
	store := fundedStore(t)
	provider := &spyProvider{payoutResult: service.PayoutResult{ProviderID: providerReference}}
	service := newService(store, provider)
	first, err := service.CreatePayout(context.Background(), payoutInput())
	if err != nil {
		t.Fatalf("CreatePayout() error = %v", err)
	}
	store.payouts.hasKey = false
	store.onLock = func() { store.payouts.hasKey = true }

	second, err := service.CreatePayout(context.Background(), payoutInput())
	if err != nil {
		t.Fatalf("second CreatePayout() error = %v", err)
	}

	if second.ID != first.ID || !second.IsReplay || len(store.payouts.items) != 1 || len(provider.payoutRequests) != 1 {
		t.Errorf("replay = %+v, payouts = %d, provider calls = %d", second, len(store.payouts.items), len(provider.payoutRequests))
	}
}

func TestCreatePayout_ReplayLookupFailsAfterLock_ReturnsError(t *testing.T) {
	store := fundedStore(t)
	store.onLock = func() { store.failures["payout.byKey"] = errBoom }
	service := newService(store, &spyProvider{payoutResult: service.PayoutResult{ProviderID: providerReference}})

	_, err := service.CreatePayout(context.Background(), payoutInput())

	if !errors.Is(err, errBoom) || len(store.payouts.items) != 0 {
		t.Errorf("err = %v, payouts = %d", err, len(store.payouts.items))
	}
}
