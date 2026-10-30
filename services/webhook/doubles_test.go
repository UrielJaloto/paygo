package webhook_test

import (
	"context"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/webhook"
	service "github.com/UrielJaloto/paygo/services/webhook"
)

type fakeEndpoints struct {
	found   map[webhook.EndpointID]webhook.Endpoint
	active  int
	saved   []webhook.Endpoint
	updated []webhook.Endpoint
	errs    map[string]error
}

func (f *fakeEndpoints) Save(_ context.Context, endpoint webhook.Endpoint) error {
	f.saved = append(f.saved, endpoint)
	return f.errs["save"]
}

func (f *fakeEndpoints) FindByID(_ context.Context, id webhook.EndpointID) (webhook.Endpoint, error) {
	if err := f.errs["find"]; err != nil {
		return webhook.Endpoint{}, err
	}
	endpoint, ok := f.found[id]
	if !ok {
		return webhook.Endpoint{}, webhook.ErrEndpointNotFound
	}
	return endpoint, nil
}

func (f *fakeEndpoints) ListByOwner(context.Context, apikey.OwnerID) ([]webhook.Endpoint, error) {
	listed := make([]webhook.Endpoint, 0, len(f.found))
	for _, endpoint := range f.found {
		listed = append(listed, endpoint)
	}
	return listed, f.errs["list"]
}

func (f *fakeEndpoints) CountActiveByOwner(context.Context, apikey.OwnerID) (int, error) {
	return f.active, f.errs["count"]
}

func (f *fakeEndpoints) Update(_ context.Context, endpoint webhook.Endpoint) error {
	f.updated = append(f.updated, endpoint)
	return f.errs["update"]
}

type fakeDeliveries struct {
	found   map[webhook.DeliveryID]webhook.Delivery
	pending []webhook.Delivery
	listed  []webhook.Delivery
	filter  service.DeliveryFilter
	limit   page.Limit
	updated []webhook.Delivery
	errs    map[string]error
}

func (f *fakeDeliveries) FindByID(_ context.Context, id webhook.DeliveryID) (webhook.Delivery, error) {
	if err := f.errs["find"]; err != nil {
		return webhook.Delivery{}, err
	}
	delivery, ok := f.found[id]
	if !ok {
		return webhook.Delivery{}, webhook.ErrDeliveryNotFound
	}
	return delivery, nil
}

func (f *fakeDeliveries) ListByOwner(_ context.Context, filter service.DeliveryFilter, _ page.Cursor, limit page.Limit) ([]webhook.Delivery, error) {
	f.filter = filter
	f.limit = limit
	return f.listed, f.errs["list"]
}

func (f *fakeDeliveries) ListPendingByEndpoint(context.Context, webhook.EndpointID) ([]webhook.Delivery, error) {
	return f.pending, f.errs["pending"]
}

func (f *fakeDeliveries) Update(_ context.Context, delivery webhook.Delivery) error {
	f.updated = append(f.updated, delivery)
	return f.errs["update"]
}

type fakeUnitOfWork struct {
	endpoints  *fakeEndpoints
	deliveries *fakeDeliveries
	runs       int
	err        error
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store service.Store) error) error {
	f.runs++
	if f.err != nil {
		return f.err
	}
	return work(ctx, service.Store{Endpoints: f.endpoints, Deliveries: f.deliveries})
}

type stubSecrets struct {
	secret string
	err    error
}

func (s stubSecrets) NewSecret() (string, error) {
	return s.secret, s.err
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

type stubIDs struct{}

func (stubIDs) NewID() string {
	return "new-id"
}
