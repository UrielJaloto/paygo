package dispatch_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/event"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	service "github.com/UrielJaloto/paygo/services/dispatch"
)

const (
	ownerOne apikey.OwnerID = "0190c000-0000-7000-8000-000000000001"
	ownerTwo apikey.OwnerID = "0190c000-0000-7000-8000-000000000002"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func occurred(id event.ID, owners ...apikey.OwnerID) event.Event {
	return event.NewTransferEvent(event.Metadata{ID: id, At: now}, transfer.Transfer{}, owners)
}

func endpoint(id webhook.EndpointID, owner apikey.OwnerID) webhook.Endpoint {
	return webhook.RestoreEndpoint(webhook.EndpointSnapshot{ID: id, OwnerID: owner, IsActive: true, CreatedAt: now})
}

func claim(id string, status webhook.DeliveryStatus, attempts int) service.Claim {
	delivery := webhook.RestoreDelivery(webhook.DeliverySnapshot{ID: webhook.DeliveryID(id), EventID: "evt", EndpointID: webhook.EndpointID(id), OwnerID: ownerOne, Status: status, Attempts: attempts, NextAttemptAt: now})
	return service.Claim{Delivery: delivery, Endpoint: endpoint(webhook.EndpointID(id), ownerOne), Event: occurred("evt", ownerOne)}
}

func retryPolicy(t *testing.T) webhook.RetryPolicy {
	t.Helper()
	policy, err := webhook.NewRetryPolicy(3)
	if err != nil {
		t.Fatalf("NewRetryPolicy: %v", err)
	}
	return policy
}

type fanOutFixture struct {
	outbox     *spyOutbox
	endpoints  *spyEndpoints
	deliveries *spyDeliveries
	svc        *service.Service
}

func newFanOutFixture() fanOutFixture {
	fixture := fanOutFixture{outbox: &spyOutbox{}, endpoints: &spyEndpoints{}, deliveries: &spyDeliveries{}}
	unitOfWork := &fakeUnitOfWork{store: service.Store{Outbox: fixture.outbox, Endpoints: fixture.endpoints, Deliveries: fixture.deliveries, IDs: &sequentialIDs{}}}
	fixture.svc = service.NewMoney(unitOfWork, &fakeQueue{}, &stubSender{}, stubClock{now: now}, stubJitter{})
	return fixture
}

func newDeliverFixture(queue *fakeQueue, sender service.Sender, jitter float64) *service.Service {
	return service.NewMoney(&fakeUnitOfWork{}, queue, sender, stubClock{now: now}, stubJitter{value: jitter})
}

func TestFanOut_TwoEvents_CreatesOneDeliveryPerOwnerEndpoint(t *testing.T) {
	fixture := newFanOutFixture()
	fixture.outbox.events = []event.Event{occurred("e1", ownerOne, ownerTwo), occurred("e2", ownerOne)}
	fixture.endpoints.endpoints = []webhook.Endpoint{endpoint("a", ownerOne), endpoint("b", ownerOne), endpoint("c", ownerTwo)}
	got, err := fixture.svc.FanOut(context.Background(), 100)
	if err != nil {
		t.Fatalf("FanOut: %v", err)
	}
	if got != (service.FanOutResult{Events: 2, Deliveries: 5}) || len(fixture.deliveries.saved) != 5 {
		t.Fatalf("got %+v, saved %d", got, len(fixture.deliveries.saved))
	}
	if len(fixture.outbox.published) != 2 || fixture.outbox.published[0] != "e1" || fixture.outbox.published[1] != "e2" {
		t.Fatalf("published = %v", fixture.outbox.published)
	}
}

func TestFanOut_NoEvents_DoesNotQueryEndpoints(t *testing.T) {
	fixture := newFanOutFixture()
	got, err := fixture.svc.FanOut(context.Background(), 100)
	if err != nil || got != (service.FanOutResult{}) || fixture.endpoints.calls != 0 {
		t.Fatalf("got %+v, err %v, calls %d", got, err, fixture.endpoints.calls)
	}
}

func TestFanOut_OwnerWithoutEndpoints_StillPublishes(t *testing.T) {
	fixture := newFanOutFixture()
	fixture.outbox.events = []event.Event{occurred("e1", ownerOne)}
	got, err := fixture.svc.FanOut(context.Background(), 100)
	if err != nil || got != (service.FanOutResult{Events: 1}) || len(fixture.outbox.published) != 1 {
		t.Fatalf("got %+v, err %v, published %v", got, err, fixture.outbox.published)
	}
}

func TestFanOut_CollaboratorFails_ReturnsWrappedError(t *testing.T) {
	cases := []struct {
		name          string
		set           func(fanOutFixture)
		wantPublished bool
	}{
		{name: "claim", set: func(f fanOutFixture) { f.outbox.claimErr = errBoom }},
		{name: "endpoints", set: func(f fanOutFixture) { f.endpoints.err = errBoom }},
		{name: "save", set: func(f fanOutFixture) { f.deliveries.err = errBoom }},
		{name: "publish", set: func(f fanOutFixture) { f.outbox.markErr = errBoom }, wantPublished: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFanOutFixture()
			fixture.outbox.events = []event.Event{occurred("e1", ownerOne)}
			testCase.set(fixture)
			_, err := fixture.svc.FanOut(context.Background(), 100)
			if !errors.Is(err, errBoom) || (fixture.outbox.published != nil) != testCase.wantPublished {
				t.Fatalf("err %v, published %v", err, fixture.outbox.published)
			}
		})
	}
}

func TestDeliverDue_SendOutcome_SettlesDelivery(t *testing.T) {
	cases := []struct {
		name     string
		attempts int
		sendErr  error
		want     service.DeliverResult
		status   webhook.DeliveryStatus
	}{
		{name: "success", want: service.DeliverResult{Succeeded: 1}, status: webhook.DeliverySucceeded},
		{name: "retry", sendErr: errBoom, want: service.DeliverResult{Retried: 1}, status: webhook.DeliveryPending},
		{name: "dead", attempts: 2, sendErr: errBoom, want: service.DeliverResult{Dead: 1}, status: webhook.DeliveryDead},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			queue := &fakeQueue{claims: []service.Claim{claim("d1", webhook.DeliveryPending, testCase.attempts)}}
			sender := &stubSender{failures: map[webhook.EndpointID]error{"d1": testCase.sendErr}}
			got, err := newDeliverFixture(queue, sender, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)})
			if err != nil || got != testCase.want || queue.updated[0].Status() != testCase.status {
				t.Fatalf("got %+v, err %v, updated %+v", got, err, queue.updated)
			}
		})
	}
}

func TestDeliverDue_FirstFailure_RetriesAfterOneSecond(t *testing.T) {
	queue := &fakeQueue{claims: []service.Claim{claim("d1", webhook.DeliveryPending, 0)}}
	sender := &stubSender{failures: map[webhook.EndpointID]error{"d1": errBoom}}
	if _, err := newDeliverFixture(queue, sender, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)}); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if want := now.Add(time.Second); !queue.updated[0].NextAttemptAt().Equal(want) {
		t.Fatalf("next = %v, want %v", queue.updated[0].NextAttemptAt(), want)
	}
}

func TestDeliverDue_LongError_TruncatesTo500Characters(t *testing.T) {
	queue := &fakeQueue{claims: []service.Claim{claim("d1", webhook.DeliveryPending, 0)}}
	sender := &stubSender{failures: map[webhook.EndpointID]error{"d1": errors.New(strings.Repeat("é", 600))}}
	if _, err := newDeliverFixture(queue, sender, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)}); err != nil {
		t.Fatalf("DeliverDue: %v", err)
	}
	if got := utf8.RuneCountInString(queue.updated[0].LastError()); got != 500 {
		t.Fatalf("length = %d", got)
	}
}

func TestDeliverDue_MixedClaims_CountsEachOutcome(t *testing.T) {
	queue := &fakeQueue{claims: []service.Claim{claim("d1", webhook.DeliveryPending, 0), claim("d2", webhook.DeliveryPending, 0), claim("d3", webhook.DeliveryPending, 0)}}
	sender := &stubSender{failures: map[webhook.EndpointID]error{"d2": errBoom}}
	got, err := newDeliverFixture(queue, sender, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)})
	if err != nil || got != (service.DeliverResult{Succeeded: 2, Retried: 1}) || len(queue.updated) != 3 {
		t.Fatalf("got %+v, err %v, updates %d", got, err, len(queue.updated))
	}
}

func TestDeliverDue_ThreeClaims_SendsConcurrently(t *testing.T) {
	queue := &fakeQueue{claims: []service.Claim{claim("d1", webhook.DeliveryPending, 0), claim("d2", webhook.DeliveryPending, 0), claim("d3", webhook.DeliveryPending, 0)}}
	sender := &barrierSender{expected: 3, release: make(chan struct{})}
	got, err := newDeliverFixture(queue, sender, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)})
	if err != nil || got.Succeeded != 3 || sender.started.Load() != 3 {
		t.Fatalf("got %+v, err %v, started %d", got, err, sender.started.Load())
	}
}

func TestDeliverDue_ClaimFails_DoesNotSend(t *testing.T) {
	queue := &fakeQueue{claimErr: errBoom}
	sender := &stubSender{}
	_, err := newDeliverFixture(queue, sender, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)})
	if !errors.Is(err, errBoom) || sender.calls.Load() != 0 {
		t.Fatalf("err %v, calls %d", err, sender.calls.Load())
	}
}

func TestDeliverDue_OneUpdateFails_AttemptsTheOthers(t *testing.T) {
	queue := &fakeQueue{claims: []service.Claim{claim("d1", webhook.DeliveryPending, 0), claim("d2", webhook.DeliveryPending, 0)}, failingID: "d1"}
	got, err := newDeliverFixture(queue, &stubSender{}, 0).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)})
	if !errors.Is(err, errBoom) || got.Succeeded != 1 || len(queue.updated) != 2 {
		t.Fatalf("got %+v, err %v, updates %d", got, err, len(queue.updated))
	}
}

func TestDeliverDue_RejectedTransition_ReturnsDomainError(t *testing.T) {
	cases := []struct {
		name    string
		status  webhook.DeliveryStatus
		sendErr error
		jitter  float64
		want    error
	}{
		{name: "success on settled delivery", status: webhook.DeliveryDead, want: webhook.ErrDeliveryNotPending},
		{name: "invalid jitter", status: webhook.DeliveryPending, sendErr: errBoom, jitter: 1, want: webhook.ErrInvalidJitter},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			queue := &fakeQueue{claims: []service.Claim{claim("d1", testCase.status, 0)}}
			sender := &stubSender{failures: map[webhook.EndpointID]error{"d1": testCase.sendErr}}
			_, err := newDeliverFixture(queue, sender, testCase.jitter).DeliverDue(context.Background(), service.DeliverInput{Policy: retryPolicy(t)})
			if !errors.Is(err, testCase.want) || len(queue.updated) != 0 {
				t.Fatalf("err %v, updates %d", err, len(queue.updated))
			}
		})
	}
}
