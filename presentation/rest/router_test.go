package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/presentation/rest"
	"github.com/gin-gonic/gin"
)

const generatedRequestID = "generated-id"

var errBoom = errors.New("boom")

type requestIDKey struct{}

type stubIDs struct{}

func (stubIDs) NewID() string { return generatedRequestID }

type contextualizer struct{}

func (contextualizer) AttachRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

type spyPanics struct {
	recovered any
	stack     []byte
	requestID any
}

func (s *spyPanics) ReportPanic(ctx context.Context, recovered any, stack []byte) {
	s.recovered, s.stack, s.requestID = recovered, stack, ctx.Value(requestIDKey{})
}

type stubReadiness bool

func (s stubReadiness) IsReady(context.Context) bool { return bool(s) }

type problemBody struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func newRouter(panics *spyPanics, ready bool) *gin.Engine {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "metrics body")
	})
	return rest.NewRouter(rest.Dependencies{
		Middleware: rest.Middleware{
			IDs:            stubIDs{},
			RequestContext: contextualizer{},
			Panics:         panics,
			RequestTimeout: time.Minute,
		},
		Controllers: rest.Controllers{Health: rest.NewHealthController(stubReadiness(ready), metrics)},
	})
}

func send(router http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func get(router http.Handler, path string) *httptest.ResponseRecorder {
	return send(router, httptest.NewRequest(http.MethodGet, path, nil))
}

func decodeProblem(t *testing.T, recorder *httptest.ResponseRecorder) problemBody {
	t.Helper()
	if got := recorder.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var body problemBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding problem %q: %v", recorder.Body.String(), err)
	}
	return body
}

func TestRouter_UnknownRoute_Returns404Problem(t *testing.T) {
	recorder := get(newRouter(&spyPanics{}, true), "/nope")
	body := decodeProblem(t, recorder)
	if recorder.Code != http.StatusNotFound || body.Type != "/problems/not-found" || body.Status != http.StatusNotFound {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
}

func TestRouter_WrongMethod_Returns404Problem(t *testing.T) {
	recorder := send(newRouter(&spyPanics{}, true), httptest.NewRequest(http.MethodDelete, "/healthz", nil))
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusNotFound || body.Type != "/problems/not-found" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
}

func TestNewRequestIDMiddleware_IncomingHeader_KeptOrReplaced(t *testing.T) {
	cases := []struct {
		name     string
		incoming string
		want     string
	}{
		{name: "valid", incoming: "abc-1", want: "abc-1"},
		{name: "longest valid", incoming: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
		{name: "absent", incoming: "", want: generatedRequestID},
		{name: "with space", incoming: "a b", want: generatedRequestID},
		{name: "too long", incoming: strings.Repeat("a", 65), want: generatedRequestID},
		{name: "invalid character", incoming: "abc_1", want: generatedRequestID},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			request.Header.Set("X-Request-Id", testCase.incoming)
			got := send(newRouter(&spyPanics{}, true), request).Header().Get("X-Request-Id")
			if got != testCase.want {
				t.Fatalf("X-Request-Id = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRecovery_HandlerPanics_Returns500AndReports(t *testing.T) {
	panics := &spyPanics{}
	router := newRouter(panics, true)
	router.GET("/panic", func(*gin.Context) { panic("kaboom") })

	recorder := get(router, "/panic")

	body := decodeProblem(t, recorder)
	if recorder.Code != http.StatusInternalServerError || body.Type != "/problems/internal" || body.Detail != "internal error" {
		t.Fatalf("code = %d, body = %+v", recorder.Code, body)
	}
	if panics.recovered != "kaboom" || len(panics.stack) == 0 || panics.requestID != generatedRequestID {
		t.Fatalf("reported %v, stack %d bytes, request id %v", panics.recovered, len(panics.stack), panics.requestID)
	}
}

func TestTimeout_Handler_SeesDeadline(t *testing.T) {
	router := newRouter(&spyPanics{}, true)
	var remaining time.Duration
	var hasDeadline bool
	router.GET("/deadline", func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		remaining, hasDeadline = time.Until(deadline), ok
	})

	get(router, "/deadline")

	if !hasDeadline || remaining <= 0 || remaining > time.Minute {
		t.Fatalf("deadline set = %v, remaining = %v", hasDeadline, remaining)
	}
}

func TestHealth_Endpoints_ReturnStatus(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		ready bool
		code  int
		body  string
	}{
		{name: "healthz", path: "/healthz", ready: false, code: http.StatusOK, body: `{"status":"ok"}`},
		{name: "ready", path: "/readyz", ready: true, code: http.StatusOK, body: `{"status":"ready"}`},
		{name: "unavailable", path: "/readyz", ready: false, code: http.StatusServiceUnavailable, body: `{"status":"unavailable"}`},
		{name: "metrics", path: "/metrics", ready: true, code: http.StatusOK, body: "metrics body"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := get(newRouter(&spyPanics{}, testCase.ready), testCase.path)
			if recorder.Code != testCase.code || recorder.Body.String() != testCase.body {
				t.Fatalf("got %d %q", recorder.Code, recorder.Body.String())
			}
		})
	}
}
