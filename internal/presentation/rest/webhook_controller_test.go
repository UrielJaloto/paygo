package rest_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/domain/webhook"
	"github.com/UrielJaloto/paygo/internal/presentation/rest"
	webhookservice "github.com/UrielJaloto/paygo/internal/services/webhook"
	"github.com/gin-gonic/gin"
)

const (
	endpointJSON   = `{"id":"end-1","url":"https://example.com/hook","is_active":true,"created_at":"2026-01-02T03:04:05.678Z"}`
	registeredJSON = `{"id":"end-1","url":"https://example.com/hook","is_active":true,"created_at":"2026-01-02T03:04:05.678Z","secret":"whsec"}`
	deliveryJSON   = `{"id":"del-1","event_id":"evt-1","endpoint_id":"end-1","status":"dead","attempts":8,"next_attempt_at":"2026-01-02T03:04:05.678Z","last_error":"timeout"}`
)

var webhookAt = time.Date(2026, 1, 2, 3, 4, 5, 678_000_000, time.UTC)

type stubWebhooks struct {
	err      error
	register webhookservice.RegisterInput
	ownerID  string
	disable  webhookservice.EndpointQuery
	list     webhookservice.DeliveryListInput
	replay   webhookservice.DeliveryQuery
	page     webhookservice.DeliveryPage
}

func webhookEndpoint() webhookservice.EndpointOutput {
	return webhookservice.EndpointOutput{ID: "end-1", URL: "https://example.com/hook", IsActive: true, CreatedAt: webhookAt}
}

func webhookDelivery() webhookservice.DeliveryOutput {
	return webhookservice.DeliveryOutput{
		ID:            "del-1",
		EventID:       "evt-1",
		EndpointID:    "end-1",
		Status:        "dead",
		Attempts:      8,
		NextAttemptAt: webhookAt,
		LastError:     "timeout",
	}
}

func (s *stubWebhooks) Register(_ context.Context, input webhookservice.RegisterInput) (webhookservice.RegisteredEndpoint, error) {
	s.register = input
	return webhookservice.RegisteredEndpoint{EndpointOutput: webhookEndpoint(), Secret: "whsec"}, s.err
}

func (s *stubWebhooks) ListEndpoints(_ context.Context, ownerID string) ([]webhookservice.EndpointOutput, error) {
	s.ownerID = ownerID
	return []webhookservice.EndpointOutput{webhookEndpoint()}, s.err
}

func (s *stubWebhooks) Disable(_ context.Context, query webhookservice.EndpointQuery) error {
	s.disable = query
	return s.err
}

func (s *stubWebhooks) ListDeliveries(_ context.Context, input webhookservice.DeliveryListInput) (webhookservice.DeliveryPage, error) {
	s.list = input
	return s.page, s.err
}

func (s *stubWebhooks) Replay(_ context.Context, query webhookservice.DeliveryQuery) (webhookservice.DeliveryOutput, error) {
	s.replay = query
	return webhookDelivery(), s.err
}

func webhookRouter(webhooks *stubWebhooks) *gin.Engine {
	controller := rest.NewWebhookController(webhooks, webhooks, webhooks, rest.WebhookPresenter{})
	return apiRouter(rest.Controllers{Webhook: controller})
}

func TestWebhookRegister_ValidURL_Returns201WithSecret(t *testing.T) {
	webhooks := &stubWebhooks{}
	recorder := call(webhookRouter(webhooks), http.MethodPost, "/v1/webhook-endpoints", `{"url":"https://example.com/hook"}`)
	assertResponse(t, recorder, http.StatusCreated, registeredJSON)
	if want := (webhookservice.RegisterInput{OwnerID: ownerID, URL: "https://example.com/hook"}); webhooks.register != want {
		t.Fatalf("input = %+v", webhooks.register)
	}
}

func TestWebhookRegister_MalformedBody_Returns400(t *testing.T) {
	recorder := call(webhookRouter(&stubWebhooks{}), http.MethodPost, "/v1/webhook-endpoints", `{"url":1}`)
	assertProblemType(t, recorder, http.StatusBadRequest, "malformed-request")
}

func TestWebhookListEndpoints_Existing_ReturnsItemsWithoutSecret(t *testing.T) {
	webhooks := &stubWebhooks{}
	recorder := call(webhookRouter(webhooks), http.MethodGet, "/v1/webhook-endpoints", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[`+endpointJSON+`]}`)
	if strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("body leaks secret: %s", recorder.Body.String())
	}
	if webhooks.ownerID != ownerID {
		t.Fatalf("owner = %q", webhooks.ownerID)
	}
}

func TestWebhookDisable_Existing_Returns204(t *testing.T) {
	webhooks := &stubWebhooks{}
	recorder := call(webhookRouter(webhooks), http.MethodDelete, "/v1/webhook-endpoints/end-1", "")
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("code = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if want := (webhookservice.EndpointQuery{OwnerID: ownerID, EndpointID: "end-1"}); webhooks.disable != want {
		t.Fatalf("query = %+v", webhooks.disable)
	}
}

func TestWebhookListDeliveries_DeadFilter_ForwardedAndCursorReturned(t *testing.T) {
	webhooks := &stubWebhooks{page: webhookservice.DeliveryPage{Items: []webhookservice.DeliveryOutput{webhookDelivery()}, NextCursor: "next"}}
	recorder := call(webhookRouter(webhooks), http.MethodGet, "/v1/webhook-deliveries?status=dead&cursor=c&limit=5", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[`+deliveryJSON+`],"next_cursor":"next"}`)
	want := webhookservice.DeliveryListInput{OwnerID: ownerID, Status: "dead", Cursor: "c", Limit: "5"}
	if webhooks.list != want {
		t.Fatalf("input = %+v", webhooks.list)
	}
}

func TestWebhookListDeliveries_LastPage_OmitsCursorAndKeepsEmptyItems(t *testing.T) {
	recorder := call(webhookRouter(&stubWebhooks{}), http.MethodGet, "/v1/webhook-deliveries", "")
	assertResponse(t, recorder, http.StatusOK, `{"items":[]}`)
}

func TestWebhookReplay_Dead_Returns200(t *testing.T) {
	webhooks := &stubWebhooks{}
	recorder := call(webhookRouter(webhooks), http.MethodPost, "/v1/webhook-deliveries/del-1/replay", "")
	assertResponse(t, recorder, http.StatusOK, deliveryJSON)
	if want := (webhookservice.DeliveryQuery{OwnerID: ownerID, DeliveryID: "del-1"}); webhooks.replay != want {
		t.Fatalf("query = %+v", webhooks.replay)
	}
}

func TestWebhookRoutes_ServiceError_ReturnsMappedProblem(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		err    error
		status int
		slug   string
	}{
		{"invalid url", http.MethodPost, "/v1/webhook-endpoints", webhook.ErrInvalidEndpointURL, http.StatusBadRequest, "invalid-request"},
		{"limit reached", http.MethodPost, "/v1/webhook-endpoints", webhook.ErrEndpointLimitReached, http.StatusConflict, "conflict"},
		{"list endpoints failure", http.MethodGet, "/v1/webhook-endpoints", errBoom, http.StatusInternalServerError, "internal"},
		{"disable missing", http.MethodDelete, "/v1/webhook-endpoints/end-1", webhook.ErrEndpointNotFound, http.StatusNotFound, "not-found"},
		{"invalid status", http.MethodGet, "/v1/webhook-deliveries?status=x", webhookservice.ErrInvalidDeliveryStatus, http.StatusBadRequest, "invalid-request"},
		{"not replayable", http.MethodPost, "/v1/webhook-deliveries/del-1/replay", webhook.ErrDeliveryNotReplayable, http.StatusConflict, "conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := ""
			if tc.path == "/v1/webhook-endpoints" && tc.method == http.MethodPost {
				body = `{"url":"x"}`
			}
			recorder := call(webhookRouter(&stubWebhooks{err: tc.err}), tc.method, tc.path, body)
			assertProblemType(t, recorder, tc.status, tc.slug)
		})
	}
}
