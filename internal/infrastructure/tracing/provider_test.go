package tracing_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/internal/infrastructure/tracing"
)

const (
	parentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	traceparent   = "00-" + parentTraceID + "-00f067aa0ba902b7-01"
)

func newRecorder(t *testing.T) (*tracetest.InMemoryExporter, trace.Tracer) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return exporter, provider.Tracer("test")
}

func singleSpan(t *testing.T, exporter *tracetest.InMemoryExporter) tracetest.SpanStub {
	t.Helper()
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	return spans[0]
}

func TestProvider_EmptyEndpoint_WorksWithoutTraceID(t *testing.T) {
	provider, err := tracing.NewProvider(context.Background(), "", "paygo")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ctx, span := provider.Tracer().Start(context.Background(), "ignored")
	span.End()
	if id := provider.TraceID(ctx); id != "" {
		t.Errorf("TraceID = %q, want empty", id)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestProvider_Endpoint_RecordsTraceIDAndShutsDown(t *testing.T) {
	provider, err := tracing.NewProvider(context.Background(), "127.0.0.1:1", "paygo")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ctx, span := provider.Tracer().Start(context.Background(), "recorded")
	if id := provider.TraceID(ctx); len(id) != 32 {
		t.Errorf("TraceID = %q, want 32 hex characters", id)
	}
	span.SetStatus(codes.Ok, "")
	if !span.SpanContext().IsValid() {
		t.Error("span context is invalid")
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestProvider_CanceledContext_ReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tracing.NewProvider(ctx, "127.0.0.1:1", "paygo"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want %v", err, context.Canceled)
	}
}

func TestHTTPSpanStarter_NoHeader_StartsNamedServerSpan(t *testing.T) {
	exporter, tracer := newRecorder(t)
	_, end := tracing.NewHTTPSpanStarter(tracer).StartServerSpan(context.Background(), "GET", "/v1/accounts/:id", http.Header{})
	end(http.StatusOK)
	span := singleSpan(t, exporter)
	if span.Name != "GET /v1/accounts/:id" {
		t.Errorf("name = %q", span.Name)
	}
	if span.SpanKind != trace.SpanKindServer {
		t.Errorf("kind = %v, want server", span.SpanKind)
	}
	if span.Status.Code == codes.Error {
		t.Error("200 marked as error")
	}
}

func TestHTTPSpanStarter_Traceparent_ContinuesTrace(t *testing.T) {
	exporter, tracer := newRecorder(t)
	header := http.Header{"Traceparent": []string{traceparent}}
	_, end := tracing.NewHTTPSpanStarter(tracer).StartServerSpan(context.Background(), "GET", "/health", header)
	end(http.StatusOK)
	if got := singleSpan(t, exporter).SpanContext.TraceID().String(); got != parentTraceID {
		t.Errorf("trace id = %s, want %s", got, parentTraceID)
	}
}

func TestHTTPSpanStarter_Status_MarksOnlyServerErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   codes.Code
	}{
		{name: "client error", status: http.StatusNotFound, want: codes.Unset},
		{name: "last success boundary", status: http.StatusInternalServerError - 1, want: codes.Unset},
		{name: "server error", status: http.StatusInternalServerError, want: codes.Error},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			exporter, tracer := newRecorder(t)
			_, end := tracing.NewHTTPSpanStarter(tracer).StartServerSpan(context.Background(), "GET", "/health", http.Header{})
			end(testCase.status)
			if got := singleSpan(t, exporter).Status.Code; got != testCase.want {
				t.Errorf("status = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestProviderTracer_InsideServerSpan_IsChildOfServerSpan(t *testing.T) {
	exporter, tracer := newRecorder(t)
	ctx, end := tracing.NewHTTPSpanStarter(tracer).StartServerSpan(context.Background(), "GET", "/health", http.Header{})
	decorator := tracing.NewProviderTracer(stubProvider{}, tracer)
	if _, err := decorator.GetCharge(ctx, "id"); err != nil {
		t.Fatalf("GetCharge: %v", err)
	}
	end(http.StatusOK)
	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	if spans[0].Parent.SpanID() != spans[1].SpanContext.SpanID() {
		t.Errorf("child parent = %s, want %s", spans[0].Parent.SpanID(), spans[1].SpanContext.SpanID())
	}
}
