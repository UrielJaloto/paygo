//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

const (
	testDocument    = "52998224725"
	testProvider    = "stub"
	clearingBRL     = account.ID("01900000-0000-7000-8000-000000000011")
	holdBRL         = account.ID("01900000-0000-7000-8000-000000000021")
	chargeKey       = transfer.IdempotencyKey("charge-key")
	payoutKey       = transfer.IdempotencyKey("payout-key")
	stubChargeID    = "provider-charge"
	stubPayoutID    = "provider-payout"
	listCutoffHours = 2
)

type paymentFixture struct {
	transferFixture
	charges *postgres.ChargeRepository
	payouts *postgres.PayoutRepository
}

func newPaymentFixture(t *testing.T) paymentFixture {
	t.Helper()
	base := newTransferFixture(t)
	return paymentFixture{
		transferFixture: base,
		charges:         postgres.NewChargeRepository(base.pool),
		payouts:         postgres.NewPayoutRepository(base.pool),
	}
}

func (f paymentFixture) newCharge(t *testing.T, sequence int, createdAt time.Time) payment.Charge {
	t.Helper()
	created, err := payment.NewCharge(payment.ChargeID(testID(sequence)), payment.ChargeDraft{
		OwnerID:   testOwner,
		AccountID: f.from,
		Amount:    money.NewMoney(1000, money.BRL),
		Payer:     payment.Payer{Document: testDocument, Name: "Ana"},
		Provider:  testProvider,
	}, createdAt.Add(24*time.Hour), createdAt)
	if err != nil {
		t.Fatalf("NewCharge() error = %v", err)
	}
	return created
}

func (f paymentFixture) newPayout(t *testing.T, sequence int, createdAt time.Time) payment.Payout {
	t.Helper()
	key, err := payment.ParsePixKey("email", "ana@example.com")
	if err != nil {
		t.Fatalf("ParsePixKey() error = %v", err)
	}
	hold := f.save(t, sequence+500, transfer.IdempotencyKey("hold-"+testID(sequence)))
	created, err := payment.NewPayout(payment.PayoutID(testID(sequence)), payment.PayoutDraft{
		OwnerID:        testOwner,
		AccountID:      f.from,
		Amount:         money.NewMoney(400, money.BRL),
		PixKey:         key,
		Provider:       testProvider,
		HoldTransferID: string(hold.ID()),
	}, createdAt)
	if err != nil {
		t.Fatalf("NewPayout() error = %v", err)
	}
	return created
}

func TestMigration_Payments_CreatesTablesAndSystemAccounts(t *testing.T) {
	pool := newTestPool(t)
	for _, table := range []string{"charges", "payouts", "provider_events", "provider_customers"} {
		if rows := countRows(t, pool, table); rows != 0 {
			t.Errorf("%s rows = %d, want 0", table, rows)
		}
	}
	accounts := postgres.NewAccountRepository(pool)
	for id, kind := range map[account.ID]account.Kind{clearingBRL: "provider_clearing", holdBRL: "payout_hold"} {
		found, err := accounts.FindByID(context.Background(), id)
		if err != nil {
			t.Fatalf("FindByID(%s) error = %v", id, err)
		}
		if found.Kind() != kind {
			t.Errorf("Kind(%s) = %q, want %q", id, found.Kind(), kind)
		}
	}
}

func TestChargeRepository_RoundTrip_KeepsEveryField(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	charge := fixture.newCharge(t, 100, testTime)
	if err := fixture.charges.Save(ctx, charge, chargeKey, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	settled := fixture.save(t, 101, "settled")
	charge.AttachProvider(stubChargeID, "copy", "qr")
	if _, err := charge.MarkPaid(string(settled.ID()), testTime.Add(time.Minute)); err != nil {
		t.Fatalf("MarkPaid() error = %v", err)
	}
	if err := fixture.charges.Update(ctx, charge); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	found, err := fixture.charges.FindByID(ctx, charge.ID())
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Amount() != charge.Amount() || found.Payer() != charge.Payer() || found.Reference() != charge.Reference() {
		t.Errorf("FindByID() = %+v, want %+v", found, charge)
	}
	if found.Status() != payment.ChargePaid || found.TransferID() != string(settled.ID()) {
		t.Errorf("status = %q transfer = %q, want paid %q", found.Status(), found.TransferID(), settled.ID())
	}
	if found.Pix().CopyPaste != "copy" || found.Pix().QRBase64 != "qr" || !found.Pix().ExpiresAt.Equal(charge.Pix().ExpiresAt) {
		t.Errorf("Pix() = %+v, want %+v", found.Pix(), charge.Pix())
	}
	if !found.Timeline().UpdatedAt.Equal(charge.Timeline().UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", found.Timeline().UpdatedAt, charge.Timeline().UpdatedAt)
	}
}

func TestChargeRepository_Finders_LocateSavedCharge(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	charge := fixture.newCharge(t, 100, testTime)
	charge.AttachProvider(stubChargeID, "copy", "qr")
	if err := fixture.charges.Save(ctx, charge, chargeKey, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	byProvider, err := fixture.charges.FindByProviderID(ctx, stubChargeID)
	if err != nil || byProvider.ID() != charge.ID() {
		t.Fatalf("FindByProviderID() = %q, %v, want %q", byProvider.ID(), err, charge.ID())
	}
	byKey, fingerprint, err := fixture.charges.FindByIdempotencyKey(ctx, testOwner, chargeKey)
	if err != nil || byKey.ID() != charge.ID() || fingerprint != testFingerprint {
		t.Fatalf("FindByIdempotencyKey() = %q, %q, %v", byKey.ID(), fingerprint, err)
	}
}

func TestChargeRepository_Missing_ReturnsErrChargeNotFound(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	_, idErr := fixture.charges.FindByID(ctx, payment.ChargeID(testID(999)))
	_, providerErr := fixture.charges.FindByProviderID(ctx, "missing")
	_, _, keyErr := fixture.charges.FindByIdempotencyKey(ctx, testOwner, "missing")
	for name, err := range map[string]error{"FindByID": idErr, "FindByProviderID": providerErr, "FindByIdempotencyKey": keyErr} {
		if !errors.Is(err, payment.ErrChargeNotFound) {
			t.Errorf("%s() error = %v, want ErrChargeNotFound", name, err)
		}
	}
}

func TestChargeRepository_SameKeyTwice_ReturnsConflict(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	if err := fixture.charges.Save(ctx, fixture.newCharge(t, 100, testTime), chargeKey, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	err := fixture.charges.Save(ctx, fixture.newCharge(t, 101, testTime), chargeKey, testFingerprint)
	if !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("Save() error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestChargeRepository_ListPending_FiltersAndLimits(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	cutoff := testTime.Add(listCutoffHours * time.Hour)
	saved := map[int]time.Time{100: testTime, 101: testTime.Add(time.Minute), 102: cutoff.Add(time.Minute), 103: testTime}
	for sequence, createdAt := range saved {
		charge := fixture.newCharge(t, sequence, createdAt)
		if err := fixture.charges.Save(ctx, charge, transfer.IdempotencyKey(testID(sequence)), testFingerprint); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}
	expired := fixture.newCharge(t, 103, testTime)
	if err := expired.MarkExpired(testTime); err != nil {
		t.Fatalf("MarkExpired() error = %v", err)
	}
	if err := fixture.charges.Update(ctx, expired); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	all, err := fixture.charges.ListPending(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if len(all) != 2 || all[0].ID() != payment.ChargeID(testID(100)) || all[1].ID() != payment.ChargeID(testID(101)) {
		t.Fatalf("ListPending() = %+v, want charges 100 and 101", all)
	}
	limited, err := fixture.charges.ListPending(ctx, cutoff, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("ListPending(limit 1) = %d, %v, want 1", len(limited), err)
	}
}

func TestPayoutRepository_RoundTrip_KeepsEveryField(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	payout := fixture.newPayout(t, 100, testTime)
	if err := fixture.payouts.Save(ctx, payout, payoutKey, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	final := fixture.save(t, 101, "final")
	payout.AttachProvider(stubPayoutID)
	if _, err := payout.MarkCompleted(string(final.ID()), testTime.Add(time.Minute)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := fixture.payouts.Update(ctx, payout); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	found, err := fixture.payouts.FindByID(ctx, payout.ID())
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Amount() != payout.Amount() || found.PixKey() != payout.PixKey() || found.Reference() != payout.Reference() {
		t.Errorf("FindByID() = %+v, want %+v", found, payout)
	}
	if found.Status() != payment.PayoutCompleted || found.Transfers() != payout.Transfers() {
		t.Errorf("status = %q transfers = %+v, want completed %+v", found.Status(), found.Transfers(), payout.Transfers())
	}
}

func TestPayoutRepository_Finders_LocateSavedPayout(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	payout := fixture.newPayout(t, 100, testTime)
	payout.AttachProvider(stubPayoutID)
	if err := fixture.payouts.Save(ctx, payout, payoutKey, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	byProvider, err := fixture.payouts.FindByProviderID(ctx, stubPayoutID)
	if err != nil || byProvider.ID() != payout.ID() {
		t.Fatalf("FindByProviderID() = %q, %v, want %q", byProvider.ID(), err, payout.ID())
	}
	byKey, fingerprint, err := fixture.payouts.FindByIdempotencyKey(ctx, testOwner, payoutKey)
	if err != nil || byKey.ID() != payout.ID() || fingerprint != testFingerprint {
		t.Fatalf("FindByIdempotencyKey() = %q, %q, %v", byKey.ID(), fingerprint, err)
	}
	pending, err := fixture.payouts.ListPending(ctx, testTime.Add(time.Hour), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPending() = %d, %v, want 1", len(pending), err)
	}
}

func TestPayoutRepository_Missing_ReturnsErrPayoutNotFound(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	_, idErr := fixture.payouts.FindByID(ctx, payment.PayoutID(testID(999)))
	_, providerErr := fixture.payouts.FindByProviderID(ctx, "missing")
	_, _, keyErr := fixture.payouts.FindByIdempotencyKey(ctx, testOwner, "missing")
	for name, err := range map[string]error{"FindByID": idErr, "FindByProviderID": providerErr, "FindByIdempotencyKey": keyErr} {
		if !errors.Is(err, payment.ErrPayoutNotFound) {
			t.Errorf("%s() error = %v, want ErrPayoutNotFound", name, err)
		}
	}
}

func TestPayoutRepository_SameKeyTwice_ReturnsConflict(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	if err := fixture.payouts.Save(ctx, fixture.newPayout(t, 100, testTime), payoutKey, testFingerprint); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	err := fixture.payouts.Save(ctx, fixture.newPayout(t, 101, testTime), payoutKey, testFingerprint)
	if !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("Save() error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestProviderEventLog_RecordedTwice_ReturnsTrueThenFalse(t *testing.T) {
	log := postgres.NewProviderEventLog(newTestPool(t))
	ctx := context.Background()
	for attempt, want := range []bool{true, false} {
		recorded, err := log.Record(ctx, testProvider, "event-1")
		if err != nil {
			t.Fatalf("Record() error = %v", err)
		}
		if recorded != want {
			t.Errorf("Record() attempt %d = %v, want %v", attempt, recorded, want)
		}
	}
}

func TestProviderCustomerCache_MissThenHit_ReturnsSavedID(t *testing.T) {
	cache := postgres.NewProviderCustomerCache(newTestPool(t))
	ctx := context.Background()
	missing, err := cache.Find(ctx, testProvider, testDocument)
	if err != nil || missing != "" {
		t.Fatalf("Find() = %q, %v, want empty miss", missing, err)
	}
	if err := cache.Save(ctx, testProvider, testDocument, "customer-1"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	found, err := cache.Find(ctx, testProvider, testDocument)
	if err != nil || found != "customer-1" {
		t.Fatalf("Find() = %q, %v, want customer-1", found, err)
	}
}

func TestPaymentRepositories_ClosedPool_ReturnErrors(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	cache := postgres.NewProviderCustomerCache(fixture.pool)
	log := postgres.NewProviderEventLog(fixture.pool)
	fixture.pool.Close()
	_, chargesErr := fixture.charges.ListPending(ctx, testTime, 1)
	_, payoutsErr := fixture.payouts.ListPending(ctx, testTime, 1)
	_, cacheErr := cache.Find(ctx, testProvider, testDocument)
	_, logErr := log.Record(ctx, testProvider, "event-1")
	for name, err := range map[string]error{"charges": chargesErr, "payouts": payoutsErr, "cache": cacheErr, "log": logErr} {
		if err == nil {
			t.Errorf("%s error = nil, want an error", name)
		}
	}
}

type stubProvider struct {
	next payment.ProviderEvent
}

func (*stubProvider) Name() string { return testProvider }

func (*stubProvider) CreatePixCharge(context.Context, paymentservice.ChargeRequest) (paymentservice.ChargeResult, error) {
	return paymentservice.ChargeResult{ProviderID: stubChargeID, PixCopyPaste: "copy", PixQRBase64: "qr"}, nil
}

func (*stubProvider) SendPixPayout(context.Context, paymentservice.PayoutRequest) (paymentservice.PayoutResult, error) {
	return paymentservice.PayoutResult{ProviderID: stubPayoutID}, nil
}

func (*stubProvider) GetCharge(context.Context, string) (payment.EventKind, error) {
	return payment.EventChargePaid, nil
}

func (*stubProvider) GetPayout(context.Context, string) (payment.EventKind, error) {
	return payment.EventPayoutDone, nil
}

func (p *stubProvider) ParseWebhook(context.Context, http.Header, []byte) (payment.ProviderEvent, error) {
	return p.next, nil
}

func TestPaymentService_RealDatabase_PaidChargeThenPayoutDone(t *testing.T) {
	fixture := newPaymentFixture(t)
	ctx := context.Background()
	provider := &stubProvider{}
	service := paymentservice.NewMoney(postgres.NewPaymentUnitOfWork(fixture.pool), provider, fixedClock{}, &sequentialIDs{})
	if _, err := service.CreateCharge(ctx, paymentservice.ChargeInput{
		OwnerID: string(testOwner), IdempotencyKey: "charge-1", AccountID: string(fixture.from),
		Amount: "10.00", Currency: "BRL", PayerName: "Ana", PayerDocument: testDocument,
	}); err != nil {
		t.Fatalf("CreateCharge() error = %v", err)
	}
	provider.next = payment.ProviderEvent{Provider: testProvider, EventID: "event-1", ProviderID: stubChargeID, Kind: payment.EventChargePaid}
	if err := service.HandleWebhook(ctx, paymentservice.WebhookInput{Provider: testProvider}); err != nil {
		t.Fatalf("HandleWebhook(charge) error = %v", err)
	}
	if _, err := service.CreatePayout(ctx, paymentservice.PayoutInput{
		OwnerID: string(testOwner), IdempotencyKey: "payout-1", AccountID: string(fixture.from),
		Amount: "4.00", Currency: "BRL", PixKey: "ana@example.com", PixKeyType: "email",
	}); err != nil {
		t.Fatalf("CreatePayout() error = %v", err)
	}
	ledgerRepository := postgres.NewLedgerRepository(fixture.pool)
	assertBalance(t, ledgerRepository, fixture.from, 600)
	assertBalance(t, ledgerRepository, holdBRL, 400)
	assertBalance(t, ledgerRepository, clearingBRL, -1000)
	provider.next = payment.ProviderEvent{Provider: testProvider, EventID: "event-2", ProviderID: stubPayoutID, Kind: payment.EventPayoutDone}
	if err := service.HandleWebhook(ctx, paymentservice.WebhookInput{Provider: testProvider}); err != nil {
		t.Fatalf("HandleWebhook(payout) error = %v", err)
	}
	assertBalance(t, ledgerRepository, fixture.from, 600)
	assertBalance(t, ledgerRepository, holdBRL, 0)
	assertBalance(t, ledgerRepository, clearingBRL, -600)
	mismatches, _, err := ledgerRepository.ListBalanceMismatches(ctx)
	if err != nil {
		t.Fatalf("CheckBalances() error = %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("CheckBalances() mismatches = %+v, want none", mismatches)
	}
}
