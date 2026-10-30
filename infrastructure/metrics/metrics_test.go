package metrics_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	domaintransfer "github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/infrastructure/metrics"
	"github.com/UrielJaloto/paygo/services/dispatch"
	"github.com/UrielJaloto/paygo/services/transfer"
)

var errBoom = errors.New("boom")

func scrape(t *testing.T, registry *metrics.Registry) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	return recorder.Body.String()
}

func assertSeries(t *testing.T, body string, series ...string) {
	t.Helper()
	for _, line := range series {
		if !strings.Contains(body+"\n", line+"\n") {
			t.Errorf("series %q missing", line)
		}
	}
}

func TestRegistry_ObserveRequest_CountsAndObservesDuration(t *testing.T) {
	registry := metrics.NewRegistry()
	registry.ObserveRequest("GET", "/v1/accounts/:id", 200, 2*time.Second)
	assertSeries(t, scrape(t, registry),
		`paygo_http_requests_total{method="GET",route="/v1/accounts/:id",status="200"} 1`,
		`paygo_http_request_duration_seconds_count{method="GET",route="/v1/accounts/:id"} 1`,
		`paygo_http_request_duration_seconds_sum{method="GET",route="/v1/accounts/:id"} 2`,
	)
}

func TestRegistry_Handler_ExposesRuntimeCollectors(t *testing.T) {
	body := scrape(t, metrics.NewRegistry())
	if !strings.Contains(body, "go_goroutines") {
		t.Error("go collector missing")
	}
}

type stubTransfers struct {
	output transfer.TransferOutput
	page   transfer.TransferPage
	err    error
}

func (s stubTransfers) Transfer(context.Context, transfer.TransferInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Deposit(context.Context, transfer.FundingInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Withdraw(context.Context, transfer.FundingInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Reverse(context.Context, transfer.ReverseInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Get(context.Context, transfer.TransferQuery) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) List(context.Context, transfer.ListInput) (transfer.TransferPage, error) {
	return s.page, s.err
}

func TestTransferServiceMetrics_Movements_CountByKindAndResult(t *testing.T) {
	calls := map[string]func(*metrics.TransferServiceMetrics) (transfer.TransferOutput, error){
		"transfer": func(m *metrics.TransferServiceMetrics) (transfer.TransferOutput, error) {
			return m.Transfer(context.Background(), transfer.TransferInput{})
		},
		"deposit": func(m *metrics.TransferServiceMetrics) (transfer.TransferOutput, error) {
			return m.Deposit(context.Background(), transfer.FundingInput{})
		},
		"withdrawal": func(m *metrics.TransferServiceMetrics) (transfer.TransferOutput, error) {
			return m.Withdraw(context.Background(), transfer.FundingInput{})
		},
		"reversal": func(m *metrics.TransferServiceMetrics) (transfer.TransferOutput, error) {
			return m.Reverse(context.Background(), transfer.ReverseInput{})
		},
	}
	results := []struct {
		result string
		next   stubTransfers
	}{
		{result: "success", next: stubTransfers{output: transfer.TransferOutput{ID: "trf-1"}}},
		{result: "replay", next: stubTransfers{output: transfer.TransferOutput{ID: "trf-1", IsReplay: true}}},
		{result: "rejected", next: stubTransfers{err: fmt.Errorf("wrapped: %w", domaintransfer.ErrInsufficientFunds)}},
		{result: "error", next: stubTransfers{err: errBoom}},
	}
	for kind, call := range calls {
		for _, tt := range results {
			t.Run(kind+"/"+tt.result, func(t *testing.T) {
				registry := metrics.NewRegistry()
				output, err := call(metrics.NewTransferServiceMetrics(tt.next, registry))
				if !errors.Is(err, tt.next.err) {
					t.Fatalf("err = %v, want %v", err, tt.next.err)
				}
				if !reflect.DeepEqual(output, tt.next.output) {
					t.Errorf("output = %+v, want %+v", output, tt.next.output)
				}
				assertSeries(t, scrape(t, registry), fmt.Sprintf(`paygo_transfers_total{kind="%s",result="%s"} 1`, kind, tt.result))
			})
		}
	}
}

func TestTransferServiceMetrics_Queries_PassThroughWithoutCounting(t *testing.T) {
	registry := metrics.NewRegistry()
	next := stubTransfers{output: transfer.TransferOutput{ID: "trf-1"}, page: transfer.TransferPage{NextCursor: "next"}, err: errBoom}
	decorator := metrics.NewTransferServiceMetrics(next, registry)
	output, err := decorator.Get(context.Background(), transfer.TransferQuery{})
	if !errors.Is(err, errBoom) || output.ID != "trf-1" {
		t.Errorf("Get = %+v, %v", output, err)
	}
	page, err := decorator.List(context.Background(), transfer.ListInput{})
	if !errors.Is(err, errBoom) || page.NextCursor != "next" {
		t.Errorf("List = %+v, %v", page, err)
	}
	if strings.Contains(scrape(t, registry), "paygo_transfers_total") {
		t.Error("queries were counted")
	}
}

type stubDispatch struct {
	fanOut  dispatch.FanOutResult
	deliver dispatch.DeliverResult
	err     error
}

func (s stubDispatch) FanOut(context.Context, int) (dispatch.FanOutResult, error) {
	return s.fanOut, s.err
}

func (s stubDispatch) DeliverDue(context.Context, dispatch.DeliverInput) (dispatch.DeliverResult, error) {
	return s.deliver, s.err
}

func TestDispatchServiceMetrics_DeliverDue_AddsEveryResult(t *testing.T) {
	registry := metrics.NewRegistry()
	want := dispatch.DeliverResult{Succeeded: 2, Retried: 1, Dead: 1}
	decorator := metrics.NewDispatchServiceMetrics(stubDispatch{deliver: want, err: errBoom}, registry)
	got, err := decorator.DeliverDue(context.Background(), dispatch.DeliverInput{})
	if !errors.Is(err, errBoom) || got != want {
		t.Fatalf("DeliverDue = %+v, %v", got, err)
	}
	assertSeries(t, scrape(t, registry),
		`paygo_webhook_deliveries_total{result="succeeded"} 2`,
		`paygo_webhook_deliveries_total{result="retry"} 1`,
		`paygo_webhook_deliveries_total{result="dead"} 1`,
	)
}

func TestDispatchServiceMetrics_FanOut_AddsPublishedEvents(t *testing.T) {
	registry := metrics.NewRegistry()
	want := dispatch.FanOutResult{Events: 3, Deliveries: 5}
	decorator := metrics.NewDispatchServiceMetrics(stubDispatch{fanOut: want, err: errBoom}, registry)
	got, err := decorator.FanOut(context.Background(), 10)
	if !errors.Is(err, errBoom) || got != want {
		t.Fatalf("FanOut = %+v, %v", got, err)
	}
	assertSeries(t, scrape(t, registry), "paygo_outbox_events_published_total 3")
}
