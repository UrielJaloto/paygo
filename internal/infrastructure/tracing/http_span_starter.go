package tracing

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type HTTPSpanStarter struct {
	tracer trace.Tracer
}

func NewHTTPSpanStarter(tracer trace.Tracer) HTTPSpanStarter {
	return HTTPSpanStarter{tracer: tracer}
}

func (s HTTPSpanStarter) StartServerSpan(ctx context.Context, method, route string, header http.Header) (context.Context, func(status int)) {
	parent := propagation.TraceContext{}.Extract(ctx, propagation.HeaderCarrier(header))
	spanContext, span := s.tracer.Start(parent, method+" "+route,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("http.route", route)))
	return spanContext, func(status int) {
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		span.End()
	}
}
