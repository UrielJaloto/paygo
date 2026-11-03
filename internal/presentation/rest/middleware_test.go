package rest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/internal/presentation/rest"
	apikeyservice "github.com/UrielJaloto/paygo/internal/services/apikey"
	"github.com/gin-gonic/gin"
)

const (
	clockStep  = 5 * time.Millisecond
	adminToken = "admin-token"
	ownerToken = "owner-token"
	ownerID    = "owner-1"
)

type steppingClock struct {
	now time.Time
}

func (c *steppingClock) Now() time.Time {
	c.now = c.now.Add(clockStep)
	return c.now
}

type observation struct {
	method   string
	route    string
	status   int
	duration time.Duration
}

type spyLogger struct {
	observation
	requestID any
	err       error
}

func (s *spyLogger) LogRequest(ctx context.Context, method, route string) func(int, time.Duration, error) {
	s.method, s.route, s.requestID = method, route, ctx.Value(requestIDKey{})
	return func(status int, duration time.Duration, err error) {
		s.status, s.duration, s.err = status, duration, err
	}
}

type spyObserver struct {
	observation
}

func (s *spyObserver) ObserveRequest(method, route string) func(int, time.Duration) {
	s.method, s.route = method, route
	return func(status int, duration time.Duration) { s.status, s.duration = status, duration }
}

type spanKey struct{}

type spySpans struct {
	observation
	header    http.Header
	spanInCtx bool
}

func (s *spySpans) StartServerSpan(ctx context.Context, method, route string, header http.Header) (context.Context, func(int)) {
	s.method, s.route, s.header = method, route, header
	return context.WithValue(ctx, spanKey{}, true), func(status int) { s.status = status }
}

type spyObservability struct {
	logger   *spyLogger
	observer *spyObserver
	spans    *spySpans
	clock    *steppingClock
}

func newSpyObservability() spyObservability {
	return spyObservability{logger: &spyLogger{}, observer: &spyObserver{}, spans: &spySpans{}, clock: &steppingClock{}}
}

func (s spyObservability) observability() rest.Observability {
	return rest.Observability{Logger: s.logger, Observer: s.observer, Spans: s.spans, Clock: s.clock}
}

type stubAuthenticator struct {
	err error
}

func (s stubAuthenticator) Authenticate(_ context.Context, token string) (apikeyservice.Principal, error) {
	if s.err != nil {
		return apikeyservice.Principal{}, s.err
	}
	if token != ownerToken {
		return apikeyservice.Principal{}, apikeyservice.ErrUnauthenticated
	}
	return apikeyservice.Principal{OwnerID: ownerID}, nil
}

type stubAdmin struct{}

func (stubAdmin) IsAdmin(token string) bool { return token == adminToken }

type spyLimiter struct {
	allow bool
	key   string
}

func (s *spyLimiter) Allow(key string) bool {
	s.key = key
	return s.allow
}

type guarded struct {
	router *gin.Engine
	called bool
	owner  string
}

func newGuarded(authenticator rest.Authenticator, limiter rest.Limiter) *guarded {
	g := &guarded{router: gin.New()}
	access := rest.Access{Authenticator: authenticator, Limiter: limiter, Admin: stubAdmin{}}
	handler := func(c *gin.Context) {
		g.called, g.owner = true, rest.OwnerFrom(c)
		c.Status(http.StatusNoContent)
	}
	rest.OwnerGroup(g.router, access).GET("/thing", handler)
	rest.AdminGroup(g.router, access).GET("/thing", handler)
	return g
}

func (g *guarded) get(path, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	return send(g.router, request)
}

func routeReturning404(spies spyObservability) *gin.Engine {
	router := routerObservedBy(spies.observability(), &spyPanics{}, true)
	router.GET("/probes/:id", func(c *gin.Context) {
		spies.spans.spanInCtx = c.Request.Context().Value(spanKey{}) == true
		c.Status(http.StatusNotFound)
	})
	return router
}

func assertObservation(t *testing.T, got observation, wantDuration time.Duration) {
	t.Helper()
	want := observation{method: http.MethodGet, route: "/probes/:id", status: http.StatusNotFound, duration: wantDuration}
	if got != want {
		t.Fatalf("observation = %+v, want %+v", got, want)
	}
}

func TestAccessLog_MatchedRoute_LogsTemplateStatusAndDuration(t *testing.T) {
	spies := newSpyObservability()
	get(routeReturning404(spies), "/probes/42")
	assertObservation(t, spies.logger.observation, clockStep*3)
	if spies.logger.requestID != generatedRequestID {
		t.Fatalf("request id = %v", spies.logger.requestID)
	}
}

func TestAccessLog_PresentedError_LogsCause(t *testing.T) {
	spies := newSpyObservability()
	router := routerObservedBy(spies.observability(), &spyPanics{}, true)
	router.GET("/fail", func(c *gin.Context) { rest.PresentError(c, errBoom) })
	get(router, "/fail")
	if !errors.Is(spies.logger.err, errBoom) || spies.logger.status != http.StatusInternalServerError {
		t.Fatalf("logged %d with %v", spies.logger.status, spies.logger.err)
	}
}

func TestMetrics_MatchedRoute_ObservesSameLabels(t *testing.T) {
	spies := newSpyObservability()
	get(routeReturning404(spies), "/probes/42")
	assertObservation(t, spies.observer.observation, clockStep)
}

func TestTracing_MatchedRoute_EndsSpanWithStatus(t *testing.T) {
	spies := newSpyObservability()
	get(routeReturning404(spies), "/probes/42")
	assertObservation(t, spies.spans.observation, 0)
	if !spies.spans.spanInCtx || spies.spans.header == nil {
		t.Fatalf("span in context = %v, header = %v", spies.spans.spanInCtx, spies.spans.header)
	}
}

func TestObservability_UnknownRoute_UsesUnmatchedLabel(t *testing.T) {
	spies := newSpyObservability()
	get(routerObservedBy(spies.observability(), &spyPanics{}, true), "/nope")
	if spies.logger.route != "unmatched" || spies.observer.route != "unmatched" || spies.spans.route != "unmatched" {
		t.Fatalf("routes = %q, %q, %q", spies.logger.route, spies.observer.route, spies.spans.route)
	}
}

func TestObservability_HandlerPanics_RecordsStatus500(t *testing.T) {
	spies := newSpyObservability()
	router := routerObservedBy(spies.observability(), &spyPanics{}, true)
	router.GET("/panic", func(*gin.Context) { panic("kaboom") })
	get(router, "/panic")
	if spies.logger.status != http.StatusInternalServerError || spies.spans.status != http.StatusInternalServerError {
		t.Fatalf("logged %d, span %d", spies.logger.status, spies.spans.status)
	}
}

func TestAuth_InvalidCredentials_Returns401(t *testing.T) {
	cases := []struct {
		name          string
		authorization string
	}{
		{"no header", ""},
		{"other scheme", "Basic x"},
		{"bearer without token", "Bearer "},
		{"bearer without separator", "Bearer"},
		{"rejected token", "Bearer wrong"},
	}
	var bodies []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newGuarded(stubAuthenticator{}, &spyLimiter{allow: true})
			recorder := g.get("/v1/thing", tc.authorization)
			body := decodeProblem(t, recorder)
			if recorder.Code != http.StatusUnauthorized || body.Type != "/problems/unauthenticated" || g.called {
				t.Fatalf("code = %d, body = %+v, called = %v", recorder.Code, body, g.called)
			}
			bodies = append(bodies, recorder.Body.String())
		})
	}
	for _, body := range bodies {
		if body != bodies[0] {
			t.Fatalf("bodies differ: %q and %q", body, bodies[0])
		}
	}
}

func TestAuth_AuthenticatorFails_Returns500(t *testing.T) {
	g := newGuarded(stubAuthenticator{err: errBoom}, &spyLimiter{allow: true})
	recorder := g.get("/v1/thing", "Bearer "+ownerToken)
	if body := decodeProblem(t, recorder); recorder.Code != http.StatusInternalServerError || body.Detail != "internal error" || g.called {
		t.Fatalf("code = %d, body = %+v, called = %v", recorder.Code, body, g.called)
	}
}

func TestAuth_ValidToken_ExposesOwnerAndRateLimitsByIt(t *testing.T) {
	limiter := &spyLimiter{allow: true}
	g := newGuarded(stubAuthenticator{}, limiter)
	recorder := g.get("/v1/thing", "Bearer "+ownerToken)
	if recorder.Code != http.StatusNoContent || g.owner != ownerID || limiter.key != ownerID {
		t.Fatalf("code = %d, owner = %q, limiter key = %q", recorder.Code, g.owner, limiter.key)
	}
}

func TestRateLimit_Denied_Returns429WithoutCallingHandler(t *testing.T) {
	g := newGuarded(stubAuthenticator{}, &spyLimiter{allow: false})
	recorder := g.get("/v1/thing", "Bearer "+ownerToken)
	body := decodeProblem(t, recorder)
	if recorder.Code != http.StatusTooManyRequests || body.Type != "/problems/rate-limited" || g.called {
		t.Fatalf("code = %d, body = %+v, called = %v", recorder.Code, body, g.called)
	}
	if got := recorder.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q", got)
	}
}

func TestRateLimit_Unauthenticated_NeverConsultsLimiter(t *testing.T) {
	limiter := &spyLimiter{allow: false}
	g := newGuarded(stubAuthenticator{}, limiter)
	if recorder := g.get("/v1/thing", ""); recorder.Code != http.StatusUnauthorized || limiter.key != "" {
		t.Fatalf("code = %d, limiter key = %q", recorder.Code, limiter.key)
	}
}

func TestAdminAuth_Token_AllowsOnlyAdmin(t *testing.T) {
	cases := []struct {
		name          string
		authorization string
		code          int
		called        bool
	}{
		{"admin", "Bearer " + adminToken, http.StatusNoContent, true},
		{"owner key", "Bearer " + ownerToken, http.StatusUnauthorized, false},
		{"missing", "", http.StatusUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newGuarded(stubAuthenticator{}, &spyLimiter{allow: true})
			recorder := g.get("/admin/v1/thing", tc.authorization)
			if recorder.Code != tc.code || g.called != tc.called {
				t.Fatalf("code = %d, called = %v", recorder.Code, g.called)
			}
		})
	}
}
