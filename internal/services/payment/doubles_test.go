package payment_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/account"
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/ledger"
	"github.com/UrielJaloto/paygo/internal/domain/money"
	"github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
	service "github.com/UrielJaloto/paygo/internal/services/payment"
)

var errBoom = errors.New("boom")

type fakeStore struct {
	onLock    func()
	accounts  map[account.ID]account.Account
	balances  map[account.ID]money.Money
	journals  []ledger.JournalTransaction
	transfers []transfer.Transfer
	charges   *fakeCharges
	payouts   *fakePayouts
	recorded  map[string]bool
	events    []event.Event
	calls     []string
	failures      map[string]error
}

func newFakeStore(accounts ...account.Account) *fakeStore {
	store := &fakeStore{
		accounts: map[account.ID]account.Account{},
		balances: map[account.ID]money.Money{},
		recorded: map[string]bool{},
		failures:     map[string]error{},
	}
	store.charges = &fakeCharges{store: store}
	store.payouts = &fakePayouts{store: store}
	for _, acc := range accounts {
		store.accounts[acc.ID()] = acc
	}
	return store
}

func (f *fakeStore) record(call string) error {
	f.calls = append(f.calls, call)
	return f.failures[call]
}

func (f *fakeStore) wasCalled(call string) bool {
	return slices.Contains(f.calls, call)
}

func (f *fakeStore) LockByIDs(_ context.Context, ids []account.ID) ([]account.Account, error) {
	if err := f.record("lock"); err != nil {
		return nil, err
	}
	if f.onLock != nil {
		f.onLock()
	}
	locked := make([]account.Account, 0, len(ids))
	for _, id := range ids {
		found, ok := f.accounts[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", account.ErrAccountNotFound, id)
		}
		locked = append(locked, found)
	}
	return locked, nil
}

func (f *fakeStore) CalculateBalance(_ context.Context, id account.ID) (money.Money, error) {
	return f.balances[id], f.record("balance")
}

func (f *fakeStore) Append(_ context.Context, journal ledger.JournalTransaction) error {
	f.journals = append(f.journals, journal)
	return f.record("append")
}

func (f *fakeStore) Save(_ context.Context, t transfer.Transfer, _ transfer.Fingerprint) error {
	f.transfers = append(f.transfers, t)
	return f.record("transfer.save")
}

func (f *fakeStore) Record(_ context.Context, provider, eventID string) (bool, error) {
	if err := f.record("event.record"); err != nil {
		return false, err
	}
	key := provider + "/" + eventID
	if f.recorded[key] {
		return false, nil
	}
	f.recorded[key] = true
	return true, nil
}

func (f *fakeStore) Add(_ context.Context, e event.Event) error {
	f.events = append(f.events, e)
	return f.record("outbox")
}

type fakeCharges struct {
	store       *fakeStore
	items       []payment.Charge
	stale       []payment.Charge
	fingerprint transfer.Fingerprint
	hasKey      bool
}

func (f *fakeCharges) Save(_ context.Context, charge payment.Charge, _ transfer.IdempotencyKey, fingerprint transfer.Fingerprint) error {
	f.items = append(f.items, charge)
	f.fingerprint = fingerprint
	f.hasKey = true
	return f.store.record("charge.save")
}

func (f *fakeCharges) Update(_ context.Context, charge payment.Charge) error {
	for i, item := range f.items {
		if item.ID() == charge.ID() {
			f.items[i] = charge
		}
	}
	return f.store.record("charge.update")
}

func (f *fakeCharges) find(match func(payment.Charge) bool) (payment.Charge, error) {
	for _, item := range f.items {
		if match(item) {
			return item, nil
		}
	}
	return payment.Charge{}, payment.ErrChargeNotFound
}

func (f *fakeCharges) FindByID(_ context.Context, id payment.ChargeID) (payment.Charge, error) {
	if err := f.store.record("charge.find"); err != nil {
		return payment.Charge{}, err
	}
	return f.find(func(c payment.Charge) bool { return c.ID() == id })
}

func (f *fakeCharges) FindByProviderID(_ context.Context, providerID string) (payment.Charge, error) {
	if err := f.store.record("charge.byProvider"); err != nil {
		return payment.Charge{}, err
	}
	return f.find(func(c payment.Charge) bool { return c.Reference().ProviderID == providerID })
}

func (f *fakeCharges) FindByIdempotencyKey(context.Context, apikey.OwnerID, transfer.IdempotencyKey) (payment.Charge, transfer.Fingerprint, error) {
	if err := f.store.record("charge.byKey"); err != nil {
		return payment.Charge{}, "", err
	}
	if !f.hasKey {
		return payment.Charge{}, "", payment.ErrChargeNotFound
	}
	return f.items[0], f.fingerprint, nil
}

func (f *fakeCharges) ListPending(context.Context, time.Time, int) ([]payment.Charge, error) {
	if err := f.store.record("charge.pending"); err != nil {
		return nil, err
	}
	if f.stale != nil {
		return f.stale, nil
	}
	var pending []payment.Charge
	for _, item := range f.items {
		if item.Status() == payment.ChargePending {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

type fakePayouts struct {
	store       *fakeStore
	items       []payment.Payout
	fingerprint transfer.Fingerprint
	hasKey      bool
}

func (f *fakePayouts) Save(_ context.Context, payout payment.Payout, _ transfer.IdempotencyKey, fingerprint transfer.Fingerprint) error {
	f.items = append(f.items, payout)
	f.fingerprint = fingerprint
	f.hasKey = true
	return f.store.record("payout.save")
}

func (f *fakePayouts) Update(_ context.Context, payout payment.Payout) error {
	for i, item := range f.items {
		if item.ID() == payout.ID() {
			f.items[i] = payout
		}
	}
	return f.store.record("payout.update")
}

func (f *fakePayouts) find(match func(payment.Payout) bool) (payment.Payout, error) {
	for _, item := range f.items {
		if match(item) {
			return item, nil
		}
	}
	return payment.Payout{}, payment.ErrPayoutNotFound
}

func (f *fakePayouts) FindByID(_ context.Context, id payment.PayoutID) (payment.Payout, error) {
	if err := f.store.record("payout.find"); err != nil {
		return payment.Payout{}, err
	}
	return f.find(func(p payment.Payout) bool { return p.ID() == id })
}

func (f *fakePayouts) FindByProviderID(_ context.Context, providerID string) (payment.Payout, error) {
	if err := f.store.record("payout.byProvider"); err != nil {
		return payment.Payout{}, err
	}
	return f.find(func(p payment.Payout) bool { return p.Reference().ProviderID == providerID })
}

func (f *fakePayouts) FindByIdempotencyKey(context.Context, apikey.OwnerID, transfer.IdempotencyKey) (payment.Payout, transfer.Fingerprint, error) {
	if err := f.store.record("payout.byKey"); err != nil {
		return payment.Payout{}, "", err
	}
	if !f.hasKey {
		return payment.Payout{}, "", payment.ErrPayoutNotFound
	}
	return f.items[0], f.fingerprint, nil
}

func (f *fakePayouts) ListPending(context.Context, time.Time, int) ([]payment.Payout, error) {
	if err := f.store.record("payout.pending"); err != nil {
		return nil, err
	}
	var pending []payment.Payout
	for _, item := range f.items {
		if item.Status() == payment.PayoutPending {
			pending = append(pending, item)
		}
	}
	return pending, nil
}

type fakeUnitOfWork struct {
	store *fakeStore
	runs  int
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store service.Store) error) error {
	f.runs++
	return work(ctx, service.Store{
		Accounts:  f.store,
		Ledger:    f.store,
		Transfers: f.store,
		Charges:   f.store.charges,
		Payouts:   f.store.payouts,
		Events:    f.store,
		Outbox:    f.store,
	})
}

type spyProvider struct {
	chargeResult   service.ChargeResult
	chargeErr      error
	payoutResult   service.PayoutResult
	payoutErr      error
	chargeRequests []service.ChargeRequest
	payoutRequests []service.PayoutRequest
	kinds          map[string]payment.EventKind
	statusFailures     map[string]error
	parsed         payment.ProviderEvent
	parseErr       error
}

func (s *spyProvider) Name() string {
	return "fake"
}

func (s *spyProvider) CreatePixCharge(_ context.Context, request service.ChargeRequest) (service.ChargeResult, error) {
	s.chargeRequests = append(s.chargeRequests, request)
	return s.chargeResult, s.chargeErr
}

func (s *spyProvider) SendPixPayout(_ context.Context, request service.PayoutRequest) (service.PayoutResult, error) {
	s.payoutRequests = append(s.payoutRequests, request)
	return s.payoutResult, s.payoutErr
}

func (s *spyProvider) GetCharge(_ context.Context, providerID string) (payment.EventKind, error) {
	return s.kinds[providerID], s.statusFailures[providerID]
}

func (s *spyProvider) GetPayout(_ context.Context, providerID string) (payment.EventKind, error) {
	return s.kinds[providerID], s.statusFailures[providerID]
}

func (s *spyProvider) ParseWebhook(context.Context, http.Header, []byte) (payment.ProviderEvent, error) {
	return s.parsed, s.parseErr
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

type sequenceIDs struct {
	next int
}

func (s *sequenceIDs) NewID() string {
	s.next++
	return fmt.Sprintf("id-%d", s.next)
}
