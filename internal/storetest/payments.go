package storetest

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

func RunPaymentContract(t *testing.T, newStores func(t *testing.T) PaymentStores) {
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
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.run(t, newStores(t))
		})
	}
}

func (s PaymentStores) newCharge(t *testing.T, sequence int, createdAt time.Time) payment.Charge {
	t.Helper()
	created, err := payment.NewCharge(payment.ChargeID(ID(sequence)), payment.ChargeDraft{
		OwnerID:   OwnerA,
		AccountID: SettlementBRL,
		Amount:    money.NewMoney(1000, money.BRL),
		Payer:     payment.Payer{Document: "52998224725", Name: "Ana"},
		Provider:  "stub",
	}, createdAt.Add(24*time.Hour), createdAt)
	NoError(t, err)
	return created
}

func (s PaymentStores) newPayout(t *testing.T, sequence int, createdAt time.Time) payment.Payout {
	t.Helper()
	key, err := payment.ParsePixKey("email", "ana@example.com")
	NoError(t, err)
	accounts := pair{from: SaveCustomer(t, s.Base, sequence+100).ID(), to: SaveCustomer(t, s.Base, sequence+200).ID()}
	hold := accounts.transfer(t, sequence+300, OwnerA)
	NoError(t, s.Base.Transfers.Save(context.Background(), hold, fingerprint))
	created, err := payment.NewPayout(payment.PayoutID(ID(sequence)), payment.PayoutDraft{
		OwnerID:        OwnerA,
		AccountID:      accounts.from,
		Amount:         money.NewMoney(400, money.BRL),
		PixKey:         key,
		Provider:       "stub",
		HoldTransferID: string(hold.ID()),
	}, createdAt)
	NoError(t, err)
	return created
}

func pendingKey(sequence int) transfer.IdempotencyKey {
	return transfer.IdempotencyKey(ID(sequence))
}

func chargeRoundTrip(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	saved := stores.newCharge(t, 1, At)
	NoError(t, stores.Charges.Save(ctx, saved, contractKey, fingerprint))
	saved.AttachProvider("provider-charge", "copy", "qr")
	NoError(t, stores.Charges.Update(ctx, saved))
	byID, err := stores.Charges.FindByID(ctx, saved.ID())
	NoError(t, err)
	Equal(t, byID.Reference().ProviderID, "provider-charge")
	Equal(t, byID.Pix().CopyPaste, "copy")
	byKey, stored, err := stores.Charges.FindByIdempotencyKey(ctx, OwnerA, contractKey)
	NoError(t, err)
	Equal(t, byKey.ID(), saved.ID())
	Equal(t, stored, fingerprint)
	byProvider, err := stores.Charges.FindByProviderID(ctx, "provider-charge")
	NoError(t, err)
	Equal(t, byProvider.ID(), saved.ID())
}

func chargeMissing(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	_, err := stores.Charges.FindByID(ctx, payment.ChargeID(ID(9)))
	ErrorIs(t, err, payment.ErrChargeNotFound)
	_, err = stores.Charges.FindByProviderID(ctx, "missing")
	ErrorIs(t, err, payment.ErrChargeNotFound)
	_, _, err = stores.Charges.FindByIdempotencyKey(ctx, OwnerA, contractKey)
	ErrorIs(t, err, payment.ErrChargeNotFound)
	NoError(t, stores.Charges.Update(ctx, stores.newCharge(t, 9, At)))
	_, err = stores.Charges.FindByID(ctx, payment.ChargeID(ID(9)))
	ErrorIs(t, err, payment.ErrChargeNotFound)
}

func chargeDuplicateKey(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	NoError(t, stores.Charges.Save(ctx, stores.newCharge(t, 1, At), contractKey, fingerprint))
	ErrorIs(t, stores.Charges.Save(ctx, stores.newCharge(t, 2, At), contractKey, fingerprint), transfer.ErrIdempotencyConflict)
}

func chargePending(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	older, newer := stores.newCharge(t, 1, At.Add(-2*time.Hour)), stores.newCharge(t, 2, At.Add(-time.Hour))
	settled := stores.newCharge(t, 3, At.Add(-3*time.Hour))
	NoError(t, stores.Charges.Save(ctx, newer, pendingKey(2), fingerprint))
	NoError(t, stores.Charges.Save(ctx, older, pendingKey(1), fingerprint))
	NoError(t, stores.Charges.Save(ctx, stores.newCharge(t, 4, At), pendingKey(4), fingerprint))
	NoError(t, stores.Charges.Save(ctx, settled, pendingKey(3), fingerprint))
	NoError(t, settled.MarkExpired(At))
	NoError(t, stores.Charges.Update(ctx, settled))
	listed, err := stores.Charges.ListPending(ctx, At, 10)
	NoError(t, err)
	Equal(t, len(listed), 2)
	Equal(t, listed[0].ID(), older.ID())
	Equal(t, listed[1].ID(), newer.ID())
	limited, err := stores.Charges.ListPending(ctx, At, 1)
	NoError(t, err)
	Equal(t, len(limited), 1)
}

func payoutRoundTrip(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	saved := stores.newPayout(t, 1, At)
	NoError(t, stores.Payouts.Save(ctx, saved, contractKey, fingerprint))
	saved.AttachProvider("provider-payout")
	NoError(t, stores.Payouts.Update(ctx, saved))
	byID, err := stores.Payouts.FindByID(ctx, saved.ID())
	NoError(t, err)
	Equal(t, byID.Reference().ProviderID, "provider-payout")
	Equal(t, byID.Transfers().Hold, saved.Transfers().Hold)
	byKey, stored, err := stores.Payouts.FindByIdempotencyKey(ctx, OwnerA, contractKey)
	NoError(t, err)
	Equal(t, byKey.ID(), saved.ID())
	Equal(t, stored, fingerprint)
	byProvider, err := stores.Payouts.FindByProviderID(ctx, "provider-payout")
	NoError(t, err)
	Equal(t, byProvider.ID(), saved.ID())
}

func payoutMissing(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	_, err := stores.Payouts.FindByID(ctx, payment.PayoutID(ID(9)))
	ErrorIs(t, err, payment.ErrPayoutNotFound)
	_, err = stores.Payouts.FindByProviderID(ctx, "missing")
	ErrorIs(t, err, payment.ErrPayoutNotFound)
	_, _, err = stores.Payouts.FindByIdempotencyKey(ctx, OwnerA, contractKey)
	ErrorIs(t, err, payment.ErrPayoutNotFound)
}

func payoutDuplicateKey(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	NoError(t, stores.Payouts.Save(ctx, stores.newPayout(t, 1, At), contractKey, fingerprint))
	ErrorIs(t, stores.Payouts.Save(ctx, stores.newPayout(t, 2, At), contractKey, fingerprint), transfer.ErrIdempotencyConflict)
}

func payoutPending(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	older, newer := stores.newPayout(t, 1, At.Add(-2*time.Hour)), stores.newPayout(t, 2, At.Add(-time.Hour))
	NoError(t, stores.Payouts.Save(ctx, newer, pendingKey(2), fingerprint))
	NoError(t, stores.Payouts.Save(ctx, older, pendingKey(1), fingerprint))
	NoError(t, stores.Payouts.Save(ctx, stores.newPayout(t, 3, At), pendingKey(3), fingerprint))
	listed, err := stores.Payouts.ListPending(ctx, At, 10)
	NoError(t, err)
	Equal(t, len(listed), 2)
	Equal(t, listed[0].ID(), older.ID())
	Equal(t, listed[1].ID(), newer.ID())
}

func providerEventOnce(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	first, err := stores.Events.Record(ctx, "stub", "event-1")
	NoError(t, err)
	Equal(t, first, true)
	repeated, err := stores.Events.Record(ctx, "stub", "event-1")
	NoError(t, err)
	Equal(t, repeated, false)
	other, err := stores.Events.Record(ctx, "other", "event-1")
	NoError(t, err)
	Equal(t, other, true)
}

func paymentRollback(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	created := stores.newCharge(t, 1, At)
	err := stores.Unit.Run(ctx, func(ctx context.Context, store paymentservice.Store) error {
		first, err := store.Events.Record(ctx, "stub", "event-1")
		NoError(t, err)
		Equal(t, first, true)
		NoError(t, store.Charges.Save(ctx, created, contractKey, fingerprint))
		return errWorkFailed
	})
	ErrorIs(t, err, errWorkFailed)
	_, err = stores.Charges.FindByID(ctx, created.ID())
	ErrorIs(t, err, payment.ErrChargeNotFound)
	NoError(t, stores.Unit.Run(ctx, func(ctx context.Context, store paymentservice.Store) error {
		first, err := store.Events.Record(ctx, "stub", "event-1")
		NoError(t, err)
		Equal(t, first, true)
		return nil
	}))
	repeated, err := stores.Events.Record(ctx, "stub", "event-1")
	NoError(t, err)
	Equal(t, repeated, false)
}

func ledgerCheckBalanced(t *testing.T, stores PaymentStores) {
	ctx := context.Background()
	_, before, err := stores.Checker.ListBalanceMismatches(ctx)
	NoError(t, err)
	accounts := newPair(t, stores.Base)
	NoError(t, stores.Base.Ledger.Append(ctx, accounts.journal(t, 100)))
	mismatches, after, err := stores.Checker.ListBalanceMismatches(ctx)
	NoError(t, err)
	Equal(t, len(mismatches), 0)
	Equal(t, after, before+2)
	totals, err := stores.Checker.CalculateTotalsByCurrency(ctx)
	NoError(t, err)
	Equal(t, totals["BRL"], 0)
	Equal(t, len(totals), 1)
}
