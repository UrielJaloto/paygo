package dispatch_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/event"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
	service "github.com/UrielJaloto/paygo/internal/services/dispatch"
)

type spyOutbox struct {
	events    []event.Event
	published []event.ID
	claimErr  error
	markErr   error
}

func (s *spyOutbox) ClaimUnpublished(context.Context, int) ([]event.Event, error) {
	return s.events, s.claimErr
}

func (s *spyOutbox) MarkPublished(_ context.Context, ids []event.ID) error {
	s.published = ids
	return s.markErr
}

type spyEndpoints struct {
	owners    []apikey.OwnerID
	endpoints []webhook.Endpoint
	calls     int
	err       error
}

func (s *spyEndpoints) ListActiveByOwners(_ context.Context, owners []apikey.OwnerID) ([]webhook.Endpoint, error) {
	s.owners = owners
	s.calls++
	return s.endpoints, s.err
}

type spyDeliveries struct {
	saved []webhook.Delivery
	err   error
}

func (s *spyDeliveries) SaveAll(_ context.Context, deliveries []webhook.Delivery) error {
	s.saved = deliveries
	return s.err
}

type sequentialIDs struct {
	next int
}

func (s *sequentialIDs) NewID() string {
	s.next++
	return fmt.Sprintf("id-%d", s.next)
}

type fakeUnitOfWork struct {
	store service.Store
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(ctx context.Context, store service.Store) error) error {
	return work(ctx, f.store)
}

type fakeQueue struct {
	claims    []service.Claim
	claimErr  error
	updated   []webhook.Delivery
	failingID webhook.DeliveryID
}

func (f *fakeQueue) ClaimDue(context.Context, time.Time, time.Duration, int) ([]service.Claim, error) {
	return f.claims, f.claimErr
}

func (f *fakeQueue) Update(_ context.Context, delivery webhook.Delivery) error {
	f.updated = append(f.updated, delivery)
	if delivery.ID() == f.failingID {
		return errBoom
	}
	return nil
}

type stubSender struct {
	failures  map[webhook.EndpointID]error
	calls atomic.Int32
}

func (s *stubSender) Send(_ context.Context, endpoint webhook.Endpoint, _ event.Event) error {
	s.calls.Add(1)
	return s.failures[endpoint.ID()]
}

type barrierSender struct {
	expected int32
	started  atomic.Int32
	release  chan struct{}
}

func (b *barrierSender) Send(context.Context, webhook.Endpoint, event.Event) error {
	if b.started.Add(1) == b.expected {
		close(b.release)
	}
	select {
	case <-b.release:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("sends did not overlap")
	}
}

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

type stubJitter struct {
	value float64
}

func (s stubJitter) Next() float64 {
	return s.value
}
