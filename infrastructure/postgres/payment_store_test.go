//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/services/ledgercheck"
	paymentservice "github.com/UrielJaloto/paygo/services/payment"
)

const contractKey transfer.IdempotencyKey = "contract-key"

type PaymentStores struct {
	Base    Stores
	Charges paymentservice.ChargeRepository
	Payouts paymentservice.PayoutRepository
	Events  paymentservice.ProviderEventLog
	Unit    paymentservice.UnitOfWork
	Checker ledgercheck.Reader
}

func runPaymentContract(t *testing.T, newStores func(t *testing.T) PaymentStores) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, stores PaymentStores)
	}{
		{"charge round trips by id, key and provider id", chargeRoundTrip},
		{"charge lookups miss with not found", chargeMissing},
		{"charge key conflicts per owner", chargeDuplicateKey},
		{"pending charges list oldest first under the cutoff", chargePending},
		{"payout round trips by id, key and provider id", payoutRoundTrip},
		{"payout lookups miss with not found", payoutMissing},
		{"payout key conflicts per owner", payoutDuplicateKey},
		{"pending payouts list oldest first under the cutoff", payoutPending},
		{"provider event records once", providerEventOnce},
		{"failed work forgets the provider event and charge", paymentRollback},
		{"ledger check sees balanced books", ledgerCheckBalanced},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newStores(t))
		})
	}
}

func (s PaymentStores) newCharge(t *testing.T, sequence int, createdAt time.Time) payment.Charge {
	t.Helper()
	created, err := payment.NewCharge(payment.ChargeID(testID(sequence)), payment.ChargeDraft{
		OwnerID:   testOwner,
		AccountID: settlementBRL,
		Amount:    money.New(1000, money.BRL),
		Payer:     payment.Payer{Document: "52998224725", Name: "Ana"},
		Provider:  "stub",
	}, createdAt.Add(24*time.Hour), createdAt)
	noError(t, err)
	return created
}

func (s PaymentStores) newPayout(t *testing.T, sequence int, createdAt time.Time) payment.Payout {
	t.Helper()
	key, err := payment.ParsePixKey("email", "ana@example.com")
	noError(t, err)
	accounts := pair{from: storeCustomer(t, s.Base, sequence+100).ID(), to: storeCustomer(t, s.Base, sequence+200).ID()}
	hold := accounts.transfer(t, sequence+300, testOwner)
	noError(t, s.Base.Transfers.Save(context.Background(), hold, testFingerprint))
	created, err := payment.NewPayout(payment.PayoutID(testID(sequence)), payment.PayoutDraft{
		OwnerID:        testOwner,
		AccountID:      accounts.from,
		Amount:         money.New(400, money.BRL),
		PixKey:         key,
		Provider:       "stub",
		HoldTransferID: string(hold.ID()),
	}, createdAt)
	noError(t, err)
	return created
}

func pendingKey(sequence int) transfer.IdempotencyKey {
	return transfer.IdempotencyKey(testID(sequence))
}

func chargeRoundTrip(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	saved := stores.newCharge(t, 1, testTime)
	noError(t, stores.Charges.Save(ctx, saved, contractKey, testFingerprint))
	saved.AttachProvider("provider-charge", "copy", "qr")
	noError(t, stores.Charges.Update(ctx, saved))
	byID, err := stores.Charges.FindByID(ctx, saved.ID())
	noError(t, err)
	equal(t, byID.Reference().ProviderID, "provider-charge")
	equal(t, byID.Pix().CopyPaste, "copy")
	byKey, stored, err := stores.Charges.FindByIdempotencyKey(ctx, testOwner, contractKey)
	noError(t, err)
	equal(t, byKey.ID(), saved.ID())
	equal(t, stored, testFingerprint)
	byProvider, err := stores.Charges.FindByProviderID(ctx, "provider-charge")
	noError(t, err)
	equal(t, byProvider.ID(), saved.ID())
}

func chargeMissing(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	_, err := stores.Charges.FindByID(ctx, payment.ChargeID(testID(9)))
	errorIs(t, err, payment.ErrChargeNotFound)
	_, err = stores.Charges.FindByProviderID(ctx, "missing")
	errorIs(t, err, payment.ErrChargeNotFound)
	_, _, err = stores.Charges.FindByIdempotencyKey(ctx, testOwner, contractKey)
	errorIs(t, err, payment.ErrChargeNotFound)
	noError(t, stores.Charges.Update(ctx, stores.newCharge(t, 9, testTime)))
	_, err = stores.Charges.FindByID(ctx, payment.ChargeID(testID(9)))
	errorIs(t, err, payment.ErrChargeNotFound)
}

func chargeDuplicateKey(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	noError(t, stores.Charges.Save(ctx, stores.newCharge(t, 1, testTime), contractKey, testFingerprint))
	errorIs(t, stores.Charges.Save(ctx, stores.newCharge(t, 2, testTime), contractKey, testFingerprint), transfer.ErrIdempotencyConflict)
}

func chargePending(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	older, newer := stores.newCharge(t, 1, testTime.Add(-2*time.Hour)), stores.newCharge(t, 2, testTime.Add(-time.Hour))
	settled := stores.newCharge(t, 3, testTime.Add(-3*time.Hour))
	noError(t, stores.Charges.Save(ctx, newer, pendingKey(2), testFingerprint))
	noError(t, stores.Charges.Save(ctx, older, pendingKey(1), testFingerprint))
	noError(t, stores.Charges.Save(ctx, stores.newCharge(t, 4, testTime), pendingKey(4), testFingerprint))
	noError(t, stores.Charges.Save(ctx, settled, pendingKey(3), testFingerprint))
	noError(t, settled.MarkExpired(testTime))
	noError(t, stores.Charges.Update(ctx, settled))
	listed, err := stores.Charges.ListPending(ctx, testTime, 10)
	noError(t, err)
	equal(t, len(listed), 2)
	equal(t, listed[0].ID(), older.ID())
	equal(t, listed[1].ID(), newer.ID())
	limited, err := stores.Charges.ListPending(ctx, testTime, 1)
	noError(t, err)
	equal(t, len(limited), 1)
}

func payoutRoundTrip(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	saved := stores.newPayout(t, 1, testTime)
	noError(t, stores.Payouts.Save(ctx, saved, contractKey, testFingerprint))
	saved.AttachProvider("provider-payout")
	noError(t, stores.Payouts.Update(ctx, saved))
	byID, err := stores.Payouts.FindByID(ctx, saved.ID())
	noError(t, err)
	equal(t, byID.Reference().ProviderID, "provider-payout")
	equal(t, byID.Transfers().Hold, saved.Transfers().Hold)
	byKey, stored, err := stores.Payouts.FindByIdempotencyKey(ctx, testOwner, contractKey)
	noError(t, err)
	equal(t, byKey.ID(), saved.ID())
	equal(t, stored, testFingerprint)
	byProvider, err := stores.Payouts.FindByProviderID(ctx, "provider-payout")
	noError(t, err)
	equal(t, byProvider.ID(), saved.ID())
}

func payoutMissing(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	_, err := stores.Payouts.FindByID(ctx, payment.PayoutID(testID(9)))
	errorIs(t, err, payment.ErrPayoutNotFound)
	_, err = stores.Payouts.FindByProviderID(ctx, "missing")
	errorIs(t, err, payment.ErrPayoutNotFound)
	_, _, err = stores.Payouts.FindByIdempotencyKey(ctx, testOwner, contractKey)
	errorIs(t, err, payment.ErrPayoutNotFound)
}

func payoutDuplicateKey(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	noError(t, stores.Payouts.Save(ctx, stores.newPayout(t, 1, testTime), contractKey, testFingerprint))
	errorIs(t, stores.Payouts.Save(ctx, stores.newPayout(t, 2, testTime), contractKey, testFingerprint), transfer.ErrIdempotencyConflict)
}

func payoutPending(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	older, newer := stores.newPayout(t, 1, testTime.Add(-2*time.Hour)), stores.newPayout(t, 2, testTime.Add(-time.Hour))
	noError(t, stores.Payouts.Save(ctx, newer, pendingKey(2), testFingerprint))
	noError(t, stores.Payouts.Save(ctx, older, pendingKey(1), testFingerprint))
	noError(t, stores.Payouts.Save(ctx, stores.newPayout(t, 3, testTime), pendingKey(3), testFingerprint))
	listed, err := stores.Payouts.ListPending(ctx, testTime, 10)
	noError(t, err)
	equal(t, len(listed), 2)
	equal(t, listed[0].ID(), older.ID())
	equal(t, listed[1].ID(), newer.ID())
}

func providerEventOnce(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	first, err := stores.Events.Record(ctx, "stub", "event-1")
	noError(t, err)
	equal(t, first, true)
	repeated, err := stores.Events.Record(ctx, "stub", "event-1")
	noError(t, err)
	equal(t, repeated, false)
	other, err := stores.Events.Record(ctx, "other", "event-1")
	noError(t, err)
	equal(t, other, true)
}

func paymentRollback(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	created := stores.newCharge(t, 1, testTime)
	err := stores.Unit.Run(ctx, func(ctx context.Context, store paymentservice.Store) error {
		first, err := store.Events.Record(ctx, "stub", "event-1")
		noError(t, err)
		equal(t, first, true)
		noError(t, store.Charges.Save(ctx, created, contractKey, testFingerprint))
		return errWorkFailed
	})
	errorIs(t, err, errWorkFailed)
	_, err = stores.Charges.FindByID(ctx, created.ID())
	errorIs(t, err, payment.ErrChargeNotFound)
	noError(t, stores.Unit.Run(ctx, func(ctx context.Context, store paymentservice.Store) error {
		first, err := store.Events.Record(ctx, "stub", "event-1")
		noError(t, err)
		equal(t, first, true)
		return nil
	}))
	repeated, err := stores.Events.Record(ctx, "stub", "event-1")
	noError(t, err)
	equal(t, repeated, false)
}

func ledgerCheckBalanced(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	_, before, err := stores.Checker.CheckBalances(ctx)
	noError(t, err)
	accounts := newPair(t, stores.Base)
	noError(t, stores.Base.Ledger.Append(ctx, accounts.journal(t, 100)))
	mismatches, after, err := stores.Checker.CheckBalances(ctx)
	noError(t, err)
	equal(t, len(mismatches), 0)
	equal(t, after, before+2)
	sums, err := stores.Checker.SumByCurrency(ctx)
	noError(t, err)
	equal(t, sums["BRL"], 0)
	equal(t, len(sums), 1)
}
