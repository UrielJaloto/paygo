package webhook_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/webhook"
	service "github.com/UrielJaloto/paygo/services/webhook"
)

const (
	ownerOne apikey.OwnerID = "0190c000-0000-7000-8000-000000000001"
	ownerTwo apikey.OwnerID = "0190c000-0000-7000-8000-000000000002"
	target                  = "https://example.com/hook"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	secret  = "whsec_" + strings.Repeat("s", 43)
)

func endpoint(id webhook.EndpointID, owner apikey.OwnerID, active bool) webhook.Endpoint {
	return webhook.RestoreEndpoint(webhook.EndpointSnapshot{ID: id, OwnerID: owner, URL: target, Secret: secret, IsActive: active, CreatedAt: now})
}

func delivery(id webhook.DeliveryID, owner apikey.OwnerID, status webhook.DeliveryStatus) webhook.Delivery {
	return webhook.RestoreDelivery(webhook.DeliverySnapshot{ID: id, EventID: "evt", EndpointID: "e1", OwnerID: owner, Status: status, Attempts: 3, NextAttemptAt: now})
}

func newFixture() (*service.Service, *fakeUnitOfWork) {
	unitOfWork := &fakeUnitOfWork{
		endpoints: &fakeEndpoints{
			found: map[webhook.EndpointID]webhook.Endpoint{"e1": endpoint("e1", ownerOne, true), "e2": endpoint("e2", ownerTwo, true), "off": endpoint("off", ownerOne, false)},
			failures:  map[string]error{},
		},
		deliveries: &fakeDeliveries{
			found: map[webhook.DeliveryID]webhook.Delivery{
				"dead":    delivery("dead", ownerOne, webhook.DeliveryDead),
				"pending": delivery("pending", ownerOne, webhook.DeliveryPending),
				"foreign": delivery("foreign", ownerTwo, webhook.DeliveryDead),
			},
			failures: map[string]error{},
		},
	}
	return service.NewMoney(unitOfWork, stubSecrets{secret: secret}, stubClock{now: now}, stubIDs{}), unitOfWork
}

func validRegister() service.RegisterInput {
	return service.RegisterInput{OwnerID: string(ownerOne), URL: target}
}

func TestRegister_Valid_ReturnsSecretAndSaves(t *testing.T) {
	svc, unitOfWork := newFixture()
	got, err := svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got.Secret != secret || got.ID != "new-id" || got.URL != target || !got.IsActive {
		t.Fatalf("got %+v", got)
	}
	if len(unitOfWork.endpoints.saved) != 1 || unitOfWork.endpoints.saved[0].Secret() != secret {
		t.Fatalf("saved = %+v", unitOfWork.endpoints.saved)
	}
}

func TestRegister_ActiveLimit_AllowsTenthRejectsEleventh(t *testing.T) {
	cases := []struct {
		name   string
		active int
		want   error
	}{
		{name: "tenth", active: 9},
		{name: "eleventh", active: 10, want: webhook.ErrEndpointLimitReached},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			unitOfWork.endpoints.active = testCase.active
			if _, err := svc.Register(context.Background(), validRegister()); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestRegister_InvalidInput_DoesNotRunUnitOfWork(t *testing.T) {
	cases := []struct {
		name  string
		input service.RegisterInput
		want  error
	}{
		{name: "owner", input: service.RegisterInput{OwnerID: "x", URL: target}, want: apikey.ErrInvalidOwner},
		{name: "url", input: service.RegisterInput{OwnerID: string(ownerOne), URL: "ftp://x"}, want: webhook.ErrInvalidEndpointURL},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			if _, err := svc.Register(context.Background(), testCase.input); !errors.Is(err, testCase.want) || unitOfWork.runs != 0 {
				t.Fatalf("error = %v, runs = %d", err, unitOfWork.runs)
			}
		})
	}
}

func TestRegister_SecretFailures_DoNotRunUnitOfWork(t *testing.T) {
	cases := []struct {
		name    string
		secrets stubSecrets
		want    error
	}{
		{name: "generator fails", secrets: stubSecrets{err: errBoom}, want: errBoom},
		{name: "malformed secret", secrets: stubSecrets{secret: "bad"}, want: webhook.ErrInvalidSecret},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			unitOfWork := &fakeUnitOfWork{}
			svc := service.NewMoney(unitOfWork, testCase.secrets, stubClock{now: now}, stubIDs{})
			if _, err := svc.Register(context.Background(), validRegister()); !errors.Is(err, testCase.want) || unitOfWork.runs != 0 {
				t.Fatalf("error = %v, runs = %d", err, unitOfWork.runs)
			}
		})
	}
}

func TestRegister_CollaboratorFails_ReturnsWrappedError(t *testing.T) {
	for _, failing := range []string{"count", "save"} {
		t.Run(failing, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			unitOfWork.endpoints.failures[failing] = errBoom
			if _, err := svc.Register(context.Background(), validRegister()); !errors.Is(err, errBoom) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestListEndpoints_Valid_HidesSecret(t *testing.T) {
	svc, _ := newFixture()
	got, err := svc.ListEndpoints(context.Background(), string(ownerOne))
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestListEndpoints_Failures_ReturnExactError(t *testing.T) {
	svc, unitOfWork := newFixture()
	if _, err := svc.ListEndpoints(context.Background(), "x"); !errors.Is(err, apikey.ErrInvalidOwner) {
		t.Fatalf("error = %v", err)
	}
	unitOfWork.endpoints.failures["list"] = errBoom
	if _, err := svc.ListEndpoints(context.Background(), string(ownerOne)); !errors.Is(err, errBoom) {
		t.Fatalf("error = %v", err)
	}
}

func TestDisable_WithPending_KillsDeliveries(t *testing.T) {
	svc, unitOfWork := newFixture()
	unitOfWork.deliveries.pending = []webhook.Delivery{delivery("d1", ownerOne, webhook.DeliveryPending), delivery("d2", ownerOne, webhook.DeliveryPending)}
	if err := svc.Disable(context.Background(), service.EndpointQuery{OwnerID: string(ownerOne), EndpointID: "e1"}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if unitOfWork.endpoints.updated[0].IsActive() || len(unitOfWork.deliveries.updated) != 2 {
		t.Fatalf("endpoints = %+v, deliveries = %+v", unitOfWork.endpoints.updated, unitOfWork.deliveries.updated)
	}
	for _, killed := range unitOfWork.deliveries.updated {
		if killed.Status() != webhook.DeliveryDead || killed.LastError() != "endpoint disabled" {
			t.Fatalf("killed = %+v", killed)
		}
	}
}

func TestDisable_Rejected_ReturnsExactError(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		id    string
		want  error
	}{
		{name: "invalid owner", owner: "x", id: "e1", want: apikey.ErrInvalidOwner},
		{name: "foreign", owner: string(ownerOne), id: "e2", want: webhook.ErrEndpointNotFound},
		{name: "missing", owner: string(ownerOne), id: "none", want: webhook.ErrEndpointNotFound},
		{name: "twice", owner: string(ownerOne), id: "off", want: webhook.ErrEndpointAlreadyDisabled},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			err := svc.Disable(context.Background(), service.EndpointQuery{OwnerID: testCase.owner, EndpointID: testCase.id})
			if !errors.Is(err, testCase.want) || len(unitOfWork.endpoints.updated) != 0 {
				t.Fatalf("error = %v, updated = %d", err, len(unitOfWork.endpoints.updated))
			}
		})
	}
}

func TestDisable_CollaboratorFails_ReturnsWrappedError(t *testing.T) {
	cases := []struct {
		name string
		set  func(*fakeUnitOfWork)
	}{
		{name: "find endpoint", set: func(u *fakeUnitOfWork) { u.endpoints.failures["find"] = errBoom }},
		{name: "update endpoint", set: func(u *fakeUnitOfWork) { u.endpoints.failures["update"] = errBoom }},
		{name: "list pending", set: func(u *fakeUnitOfWork) { u.deliveries.failures["pending"] = errBoom }},
		{name: "update delivery", set: func(u *fakeUnitOfWork) { u.deliveries.failures["update"] = errBoom }},
		{name: "unit of work", set: func(u *fakeUnitOfWork) { u.err = errBoom }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			unitOfWork.deliveries.pending = []webhook.Delivery{delivery("d1", ownerOne, webhook.DeliveryPending)}
			testCase.set(unitOfWork)
			err := svc.Disable(context.Background(), service.EndpointQuery{OwnerID: string(ownerOne), EndpointID: "e1"})
			if !errors.Is(err, errBoom) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestListDeliveries_StatusDead_FiltersAndPages(t *testing.T) {
	svc, unitOfWork := newFixture()
	unitOfWork.deliveries.listed = []webhook.Delivery{delivery("d1", ownerOne, webhook.DeliveryDead), delivery("d2", ownerOne, webhook.DeliveryDead)}
	got, err := svc.ListDeliveries(context.Background(), service.DeliveryListInput{OwnerID: string(ownerOne), Status: "dead", Limit: "1"})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if unitOfWork.deliveries.filter != (service.DeliveryFilter{Owner: ownerOne, Status: webhook.DeliveryDead}) || unitOfWork.deliveries.limit != 2 {
		t.Fatalf("filter = %+v, limit = %d", unitOfWork.deliveries.filter, unitOfWork.deliveries.limit)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "d1" || got.NextCursor != page.NewCursor("d1").Encode() {
		t.Fatalf("got %+v", got)
	}
}

func TestListDeliveries_LastPage_HasNoCursor(t *testing.T) {
	svc, unitOfWork := newFixture()
	unitOfWork.deliveries.listed = []webhook.Delivery{delivery("d1", ownerOne, webhook.DeliveryPending)}
	got, err := svc.ListDeliveries(context.Background(), service.DeliveryListInput{OwnerID: string(ownerOne)})
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(got.Items) != 1 || got.NextCursor != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestListDeliveries_Failures_ReturnExactError(t *testing.T) {
	owner := string(ownerOne)
	cases := []struct {
		name  string
		input service.DeliveryListInput
		want  error
	}{
		{name: "invalid owner", input: service.DeliveryListInput{OwnerID: "x"}, want: apikey.ErrInvalidOwner},
		{name: "invalid status", input: service.DeliveryListInput{OwnerID: owner, Status: "x"}, want: service.ErrInvalidDeliveryStatus},
		{name: "invalid cursor", input: service.DeliveryListInput{OwnerID: owner, Cursor: "%"}, want: page.ErrInvalidCursor},
		{name: "invalid limit", input: service.DeliveryListInput{OwnerID: owner, Limit: "x"}, want: page.ErrInvalidPageLimit},
		{name: "repository fails", input: service.DeliveryListInput{OwnerID: owner}, want: errBoom},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			unitOfWork.deliveries.failures["list"] = errBoom
			if _, err := svc.ListDeliveries(context.Background(), testCase.input); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestReplay_Dead_ReturnsPendingWithZeroAttempts(t *testing.T) {
	svc, unitOfWork := newFixture()
	got, err := svc.Replay(context.Background(), service.DeliveryQuery{OwnerID: string(ownerOne), DeliveryID: "dead"})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if got.Status != "pending" || got.Attempts != 0 || len(unitOfWork.deliveries.updated) != 1 {
		t.Fatalf("got %+v, updated = %d", got, len(unitOfWork.deliveries.updated))
	}
}

func TestReplay_Rejected_ReturnsExactError(t *testing.T) {
	owner := string(ownerOne)
	cases := []struct {
		name  string
		query service.DeliveryQuery
		fail  string
		want  error
	}{
		{name: "invalid owner", query: service.DeliveryQuery{OwnerID: "x"}, want: apikey.ErrInvalidOwner},
		{name: "pending", query: service.DeliveryQuery{OwnerID: owner, DeliveryID: "pending"}, want: webhook.ErrDeliveryNotReplayable},
		{name: "foreign", query: service.DeliveryQuery{OwnerID: owner, DeliveryID: "foreign"}, want: webhook.ErrDeliveryNotFound},
		{name: "find fails", query: service.DeliveryQuery{OwnerID: owner, DeliveryID: "dead"}, fail: "find", want: errBoom},
		{name: "update fails", query: service.DeliveryQuery{OwnerID: owner, DeliveryID: "dead"}, fail: "update", want: errBoom},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, unitOfWork := newFixture()
			unitOfWork.deliveries.failures[testCase.fail] = testCase.want
			if _, err := svc.Replay(context.Background(), testCase.query); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
