package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const instrumentationName = "github.com/UrielJaloto/paygo"

type Provider struct {
	tracer   trace.Tracer
	shutdown func(ctx context.Context) error
}

func NewProvider(ctx context.Context, endpoint, serviceName string) (*Provider, error) {
	if endpoint == "" {
		return &Provider{tracer: noop.NewTracerProvider().Tracer(instrumentationName), shutdown: func(context.Context) error { return nil }}, nil
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
	)
	return &Provider{tracer: provider.Tracer(instrumentationName), shutdown: provider.Shutdown}, nil
}

func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

func (p *Provider) Shutdown(ctx context.Context) error {
	return p.shutdown(ctx)
}

func (p *Provider) TraceID(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.HasTraceID() {
		return ""
	}
	return spanContext.TraceID().String()
}

func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
