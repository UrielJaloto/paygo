package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/services/dispatch"
)

type dispatchService interface {
	FanOut(ctx context.Context, limit int) (dispatch.FanOutResult, error)
	DeliverDue(ctx context.Context, input dispatch.DeliverInput) (dispatch.DeliverResult, error)
}

type DispatchServiceTracer struct {
	next   dispatchService
	tracer trace.Tracer
}

func NewDispatchServiceTracer(next dispatchService, tracer trace.Tracer) *DispatchServiceTracer {
	return &DispatchServiceTracer{next: next, tracer: tracer}
}

func (t *DispatchServiceTracer) FanOut(ctx context.Context, limit int) (dispatch.FanOutResult, error) {
	ctx, span := t.tracer.Start(ctx, "dispatch.FanOut")
	output, err := t.next.FanOut(ctx, limit)
	endSpan(span, err)
	return output, err
}

func (t *DispatchServiceTracer) DeliverDue(ctx context.Context, input dispatch.DeliverInput) (dispatch.DeliverResult, error) {
	ctx, span := t.tracer.Start(ctx, "dispatch.DeliverDue")
	output, err := t.next.DeliverDue(ctx, input)
	endSpan(span, err)
	return output, err
}
