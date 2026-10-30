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

const (
	ownerID       = "01900000-0000-7000-8000-0000000000a1"
	otherOwnerID  = "01900000-0000-7000-8000-0000000000a2"
	customerID    = "01900000-0000-7000-8000-0000000000b1"
	clearingID    = "01900000-0000-7000-8000-000000000011"
	holdID        = "01900000-0000-7000-8000-000000000021"
	validDocument = "52998224725"
	providerReference   = "fake_1"
)

var testNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func newAccount(id, owner string, kind account.Kind, currency money.Currency) account.Account {
	return account.Restore(account.Snapshot{
		ID:       account.ID(id),
		OwnerID:  apikey.OwnerID(owner),
		Currency: currency,
		Kind:     kind,
		Status:   account.StatusActive,
	})
}

func brl(t *testing.T, minor int64) money.Money {
	t.Helper()
	return money.NewMoney(minor, money.BRL)
}

func paymentStore() *fakeStore {
	return newFakeStore(
		newAccount(customerID, ownerID, account.KindCustomer, money.BRL),
		newAccount(clearingID, string(apikey.SystemOwnerID), account.KindProviderClearing, money.BRL),
		newAccount(holdID, string(apikey.SystemOwnerID), account.KindPayoutHold, money.BRL),
	)
}

func newService(store *fakeStore, provider *spyProvider) *service.Service {
	return service.NewMoney(&fakeUnitOfWork{store: store}, provider, stubClock{now: testNow}, &sequenceIDs{})
}

func chargeInput() service.ChargeInput {
	return service.ChargeInput{
		OwnerID:        ownerID,
		IdempotencyKey: "key-1",
		AccountID:      customerID,
		Amount:         "10.00",
		Currency:       "BRL",
		PayerName:      "Ana",
		PayerDocument:  validDocument,
	}
}

func seedCharge(t *testing.T, store *fakeStore, status payment.ChargeStatus) payment.Charge {
	t.Helper()
	charge := payment.RestoreCharge(payment.ChargeSnapshot{
		ID:        "charge-1",
		OwnerID:   ownerID,
		AccountID: customerID,
		Amount:    brl(t, 1000),
		Reference: payment.Reference{Provider: "fake", ProviderID: providerReference},
		Pix:       payment.PixCode{ExpiresAt: testNow.Add(24 * time.Hour)},
		Status:    status,
		Timeline:  payment.Timeline{CreatedAt: testNow, UpdatedAt: testNow},
	})
	store.charges.items = append(store.charges.items, charge)
	return charge
}

func TestCreateCharge_ValidInput_ReturnsPendingWithPixData(t *testing.T) {
	store := paymentStore()
	provider := &spyProvider{chargeResult: service.ChargeResult{ProviderID: providerReference, PixCopyPaste: "copy", PixQRBase64: "qr"}}

	got, err := newService(store, provider).CreateCharge(context.Background(), chargeInput())
	if err != nil {
		t.Fatalf("CreateCharge() error = %v", err)
	}

	want := service.ChargeOutput{
		ID:           "id-1",
		AccountID:    customerID,
		Status:       "pending",
		PixCopyPaste: "copy",
		PixQRBase64:  "qr",
		AmountMinor:  1000,
		Currency:     "BRL",
		ExpiresAt:    testNow.Add(24 * time.Hour),
		CreatedAt:    testNow,
	}
	if got != want {
		t.Errorf("CreateCharge() = %+v, want %+v", got, want)
	}
	wantRequest := service.ChargeRequest{ExternalReference: "id-1", PayerName: "Ana", PayerDocument: validDocument, AmountMinor: 1000, DueDate: testNow.Add(24 * time.Hour)}
	if len(provider.chargeRequests) != 1 || provider.chargeRequests[0] != wantRequest {
		t.Errorf("provider requests = %+v, want [%+v]", provider.chargeRequests, wantRequest)
	}
	if store.charges.items[0].Reference().ProviderID != providerReference {
		t.Errorf("stored provider id = %q, want %q", store.charges.items[0].Reference().ProviderID, providerReference)
	}
}

func TestCreateCharge_InvalidInput_ReturnsErrorWithoutCallingProvider(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*service.ChargeInput)
		want   error
	}{
		{name: "owner", mutate: func(i *service.ChargeInput) { i.OwnerID = "nope" }, want: apikey.ErrInvalidOwner},
		{name: "key", mutate: func(i *service.ChargeInput) { i.IdempotencyKey = "" }, want: transfer.ErrInvalidIdempotencyKey},
		{name: "account", mutate: func(i *service.ChargeInput) { i.AccountID = "nope" }, want: account.ErrInvalidAccountID},
		{name: "currency code", mutate: func(i *service.ChargeInput) { i.Currency = "XXX" }, want: money.ErrUnsupportedCurrency},
		{name: "currency not accepted", mutate: func(i *service.ChargeInput) { i.Currency = "USD" }, want: money.ErrUnsupportedCurrency},
		{name: "amount", mutate: func(i *service.ChargeInput) { i.Amount = "abc" }, want: money.ErrInvalidAmount},
		{name: "document", mutate: func(i *service.ChargeInput) { i.PayerDocument = "1" }, want: payment.ErrInvalidDocument},
		{name: "payer name", mutate: func(i *service.ChargeInput) { i.PayerName = "" }, want: payment.ErrInvalidPayerName},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &spyProvider{}
			input := chargeInput()
			testCase.mutate(&input)
			_, err := newService(paymentStore(), provider).CreateCharge(context.Background(), input)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("CreateCharge() error = %v, want %v", err, testCase.want)
			}
			if len(provider.chargeRequests) != 0 {
				t.Errorf("provider called %d times, want 0", len(provider.chargeRequests))
			}
		})
	}
}

func TestCreateCharge_AccountNotAllowed_ReturnsAccountNotFound(t *testing.T) {
	tests := []struct {
		name    string
		account account.Account
	}{
		{name: "other owner", account: newAccount(customerID, otherOwnerID, account.KindCustomer, money.BRL)},
		{name: "not a customer account", account: newAccount(customerID, ownerID, account.KindSettlement, money.BRL)},
		{name: "not in BRL", account: newAccount(customerID, ownerID, account.KindCustomer, money.USD)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &spyProvider{}
			store := newFakeStore(testCase.account)
			_, err := newService(store, provider).CreateCharge(context.Background(), chargeInput())
			if !errors.Is(err, account.ErrAccountNotFound) {
				t.Fatalf("CreateCharge() error = %v, want %v", err, account.ErrAccountNotFound)
			}
			if store.wasCalled("charge.save") || len(provider.chargeRequests) != 0 {
				t.Errorf("charge saved or provider called: calls %v", store.calls)
			}
		})
	}
}

func TestCreateCharge_SameKeyAndRequest_ReplaysWithoutCallingProvider(t *testing.T) {
	store := paymentStore()
	provider := &spyProvider{chargeResult: service.ChargeResult{ProviderID: providerReference}}
	svc := newService(store, provider)
	first, err := svc.CreateCharge(context.Background(), chargeInput())
	if err != nil {
		t.Fatalf("first CreateCharge() error = %v", err)
	}

	second, err := svc.CreateCharge(context.Background(), chargeInput())
	if err != nil {
		t.Fatalf("second CreateCharge() error = %v", err)
	}

	if second.ID != first.ID || !second.IsReplay {
		t.Errorf("replay = %+v, want id %s and IsReplay", second, first.ID)
	}
	if len(provider.chargeRequests) != 1 {
		t.Errorf("provider called %d times, want 1", len(provider.chargeRequests))
	}
}

func TestCreateCharge_SameKeyOtherRequest_ReturnsConflict(t *testing.T) {
	store := paymentStore()
	svc := newService(store, &spyProvider{})
	if _, err := svc.CreateCharge(context.Background(), chargeInput()); err != nil {
		t.Fatalf("first CreateCharge() error = %v", err)
	}
	input := chargeInput()
	input.Amount = "11.00"

	_, err := svc.CreateCharge(context.Background(), input)
	if !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("CreateCharge() error = %v, want %v", err, transfer.ErrIdempotencyConflict)
	}
}

func TestCreateCharge_ProviderFails_MarksChargeFailed(t *testing.T) {
	store := paymentStore()
	provider := &spyProvider{chargeErr: payment.ErrProviderUnavailable}

	_, err := newService(store, provider).CreateCharge(context.Background(), chargeInput())
	if !errors.Is(err, payment.ErrProviderUnavailable) {
		t.Fatalf("CreateCharge() error = %v, want %v", err, payment.ErrProviderUnavailable)
	}
	if got := store.charges.items[0].Status(); got != payment.ChargeFailed {
		t.Errorf("status = %s, want %s", got, payment.ChargeFailed)
	}
}

func TestCreateCharge_StoreCallFails_ReturnsWrappedError(t *testing.T) {
	for _, call := range []string{"charge.byKey", "lock", "charge.save", "charge.update"} {
		t.Run(call, func(t *testing.T) {
			store := paymentStore()
			store.failures[call] = errBoom
			_, err := newService(store, &spyProvider{}).CreateCharge(context.Background(), chargeInput())
			if !errors.Is(err, errBoom) {
				t.Fatalf("CreateCharge() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestGetCharge_Queries_ReturnOnlyOwnCharges(t *testing.T) {
	tests := []struct {
		name  string
		query service.ChargeQuery
		fail  error
		want  error
	}{
		{name: "own charge", query: service.ChargeQuery{OwnerID: ownerID, ChargeID: "charge-1"}},
		{name: "invalid owner", query: service.ChargeQuery{OwnerID: "nope", ChargeID: "charge-1"}, want: apikey.ErrInvalidOwner},
		{name: "other owner", query: service.ChargeQuery{OwnerID: otherOwnerID, ChargeID: "charge-1"}, want: payment.ErrChargeNotFound},
		{name: "unknown id", query: service.ChargeQuery{OwnerID: ownerID, ChargeID: "other"}, want: payment.ErrChargeNotFound},
		{name: "store fails", query: service.ChargeQuery{OwnerID: ownerID, ChargeID: "charge-1"}, fail: errBoom, want: errBoom},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := paymentStore()
			seedCharge(t, store, payment.ChargePending)
			store.failures["charge.find"] = testCase.fail
			got, err := newService(store, &spyProvider{}).GetCharge(context.Background(), testCase.query)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("GetCharge() error = %v, want %v", err, testCase.want)
			}
			if testCase.want == nil && got.ID != "charge-1" {
				t.Errorf("GetCharge() id = %q, want charge-1", got.ID)
			}
		})
	}
}

func webhookFor(kind payment.EventKind, eventID string) *spyProvider {
	return &spyProvider{parsed: payment.ProviderEvent{Provider: "fake", EventID: eventID, ProviderID: providerReference, Kind: kind}}
}

func assertChargePosting(t *testing.T, posted transfer.Transfer) {
	t.Helper()
	if posted.Kind() != transfer.KindCharge || posted.From() != clearingID || posted.To() != customerID || posted.Amount().Minor() != 1000 {
		t.Errorf("posted = %s %s -> %s %d", posted.Kind(), posted.From(), posted.To(), posted.Amount().Minor())
	}
	if posted.IdempotencyKey() != "charge-charge-1" {
		t.Errorf("transfer key = %q, want charge-charge-1", posted.IdempotencyKey())
	}
}

func TestHandleWebhook_ChargePaid_PostsChargeAndPublishesEvent(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)

	err := newService(store, webhookFor(payment.EventChargePaid, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
	if err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	if len(store.transfers) != 1 || len(store.journals) != 1 {
		t.Fatalf("transfers = %d, journals = %d, want 1 and 1", len(store.transfers), len(store.journals))
	}
	posted := store.transfers[0]
	assertChargePosting(t, posted)
	if len(store.events) != 1 || store.events[0].Type() != event.TypeChargePaid {
		t.Errorf("events = %+v, want one charge.paid", store.events)
	}
	if got := store.charges.items[0]; got.Status() != payment.ChargePaid || got.TransferID() != string(posted.ID()) {
		t.Errorf("charge = %s transfer %q, want paid transfer %q", got.Status(), got.TransferID(), posted.ID())
	}
}

func deliverPaid(t *testing.T, store *fakeStore, eventID string) {
	t.Helper()
	err := newService(store, webhookFor(payment.EventChargePaid, eventID)).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
	if err != nil {
		t.Fatalf("HandleWebhook(%s) error = %v", eventID, err)
	}
}

func TestHandleWebhook_RepeatedPaidEvents_PostOnce(t *testing.T) {
	tests := []struct {
		name   string
		second string
	}{
		{name: "same event twice", second: "evt-1"},
		{name: "second paid event with another id", second: "evt-2"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := paymentStore()
			seedCharge(t, store, payment.ChargePending)
			deliverPaid(t, store, "evt-1")
			deliverPaid(t, store, testCase.second)
			if len(store.transfers) != 1 || len(store.events) != 1 {
				t.Errorf("transfers = %d, events = %d, want 1 and 1", len(store.transfers), len(store.events))
			}
		})
	}
}

func TestHandleWebhook_ChargeExpired_ExpiresWithoutPosting(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)

	err := newService(store, webhookFor(payment.EventChargeExpired, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
	if err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	if got := store.charges.items[0].Status(); got != payment.ChargeExpired {
		t.Errorf("status = %s, want %s", got, payment.ChargeExpired)
	}
	if len(store.transfers) != 0 {
		t.Errorf("transfers = %d, want 0", len(store.transfers))
	}
	if len(store.events) != 1 || store.events[0].Type() != event.TypeChargeExpired {
		t.Errorf("events = %+v, want one charge.expired", store.events)
	}
}

func TestHandleWebhook_IgnoredEvent_ChangesNothing(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)

	err := newService(store, webhookFor(payment.EventIgnored, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
	if err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}
	if store.wasCalled("charge.byProvider") || len(store.events) != 0 {
		t.Errorf("calls = %v, events = %d, want nothing applied", store.calls, len(store.events))
	}
}

func TestHandleWebhook_Rejections_ReturnError(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		parseErr error
		want     error
	}{
		{name: "wrong provider name", provider: "other", want: payment.ErrUnsupportedProvider},
		{name: "authentication fails", provider: "fake", parseErr: errBoom, want: errBoom},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := paymentStore()
			provider := &spyProvider{parseErr: testCase.parseErr}
			err := newService(store, provider).HandleWebhook(context.Background(), service.WebhookInput{Provider: testCase.provider})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("HandleWebhook() error = %v, want %v", err, testCase.want)
			}
			if store.wasCalled("event.record") {
				t.Errorf("event recorded on a rejected webhook")
			}
		})
	}
}

func TestHandleWebhook_UnknownProviderID_ReturnsChargeNotFound(t *testing.T) {
	for _, kind := range []payment.EventKind{payment.EventChargePaid, payment.EventChargeExpired} {
		t.Run(string(kind), func(t *testing.T) {
			err := newService(paymentStore(), webhookFor(kind, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
			if !errors.Is(err, payment.ErrChargeNotFound) {
				t.Fatalf("HandleWebhook() error = %v, want %v", err, payment.ErrChargeNotFound)
			}
		})
	}
}

func TestHandleWebhook_StaleTransition_AcknowledgesWithoutChanges(t *testing.T) {
	tests := []struct {
		name   string
		kind   payment.EventKind
		status payment.ChargeStatus
	}{
		{name: "paid after expired", kind: payment.EventChargePaid, status: payment.ChargeExpired},
		{name: "expired after paid", kind: payment.EventChargeExpired, status: payment.ChargePaid},
		{name: "expired after failed", kind: payment.EventChargeExpired, status: payment.ChargeFailed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := paymentStore()
			seedCharge(t, store, testCase.status)
			err := newService(store, webhookFor(testCase.kind, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
			if err != nil {
				t.Fatalf("HandleWebhook() error = %v, want nil", err)
			}
			if got := store.charges.items[0].Status(); got != testCase.status {
				t.Errorf("status = %s, want %s", got, testCase.status)
			}
			if len(store.transfers) != 0 || len(store.events) != 0 || store.wasCalled("charge.update") {
				t.Errorf("transfers = %d, events = %d, calls = %v, want nothing applied", len(store.transfers), len(store.events), store.calls)
			}
		})
	}
}

func TestHandleWebhook_StoreCallFails_ReturnsWrappedError(t *testing.T) {
	for _, call := range []string{"event.record", "charge.byProvider", "lock", "append", "transfer.save", "charge.update", "outbox"} {
		t.Run(call, func(t *testing.T) {
			store := paymentStore()
			seedCharge(t, store, payment.ChargePending)
			store.failures[call] = errBoom
			err := newService(store, webhookFor(payment.EventChargePaid, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
			if !errors.Is(err, errBoom) {
				t.Fatalf("HandleWebhook() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestHandleWebhook_ExpiredStoreCallFails_ReturnsWrappedError(t *testing.T) {
	store := paymentStore()
	seedCharge(t, store, payment.ChargePending)
	store.failures["charge.byProvider"] = errBoom
	err := newService(store, webhookFor(payment.EventChargeExpired, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
	if !errors.Is(err, errBoom) {
		t.Fatalf("HandleWebhook() error = %v, want %v", err, errBoom)
	}
}

func TestHandleWebhook_ChargeCannotPost_ReturnsDomainError(t *testing.T) {
	tests := []struct {
		name      string
		accountID account.ID
		minor     int64
		want      error
	}{
		{name: "same account", accountID: clearingID, minor: 1000, want: transfer.ErrSameAccount},
		{name: "zero amount", accountID: customerID, minor: 0, want: money.ErrInvalidAmount},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			store := paymentStore()
			store.charges.items = []payment.Charge{payment.RestoreCharge(payment.ChargeSnapshot{
				ID:        "charge-1",
				AccountID: testCase.accountID,
				Amount:    brl(t, testCase.minor),
				Reference: payment.Reference{ProviderID: providerReference},
				Status:    payment.ChargePending,
			})}
			err := newService(store, webhookFor(payment.EventChargePaid, "evt-1")).HandleWebhook(context.Background(), service.WebhookInput{Provider: "fake"})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("HandleWebhook() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestCreateCharge_SameKeyCommittedWhileWaitingForLock_Replays(t *testing.T) {
	store := paymentStore()
	provider := &spyProvider{chargeResult: service.ChargeResult{ProviderID: providerReference}}
	svc := newService(store, provider)
	first, err := svc.CreateCharge(context.Background(), chargeInput())
	if err != nil {
		t.Fatalf("first CreateCharge() error = %v", err)
	}
	store.charges.hasKey = false
	store.onLock = func() { store.charges.hasKey = true }

	second, err := svc.CreateCharge(context.Background(), chargeInput())
	if err != nil {
		t.Fatalf("second CreateCharge() error = %v", err)
	}

	if second.ID != first.ID || !second.IsReplay || len(store.charges.items) != 1 || len(provider.chargeRequests) != 1 {
		t.Errorf("replay = %+v, charges = %d, provider calls = %d", second, len(store.charges.items), len(provider.chargeRequests))
	}
}

func TestCreateCharge_ReplayLookupFailsAfterLock_ReturnsError(t *testing.T) {
	store := paymentStore()
	store.onLock = func() { store.failures["charge.byKey"] = errBoom }
	svc := newService(store, &spyProvider{chargeResult: service.ChargeResult{ProviderID: providerReference}})

	_, err := svc.CreateCharge(context.Background(), chargeInput())

	if !errors.Is(err, errBoom) || len(store.charges.items) != 0 {
		t.Errorf("err = %v, charges = %d", err, len(store.charges.items))
	}
}
