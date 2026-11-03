package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type QuerySpanStarter struct {
	tracer trace.Tracer
}

func NewQuerySpanStarter(tracer trace.Tracer) QuerySpanStarter {
	return QuerySpanStarter{tracer: tracer}
}

func (s QuerySpanStarter) StartQuerySpan(ctx context.Context, sql string) (context.Context, func(err error)) {
	ctx, span := s.tracer.Start(ctx, "postgres.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.query.text", sql)))
	return ctx, func(err error) { finish(span, err) }
}
