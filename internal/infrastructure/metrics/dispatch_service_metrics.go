package metrics

import (
	"context"

	"github.com/UrielJaloto/paygo/internal/services/dispatch"
)

type dispatchService interface {
	FanOut(ctx context.Context, limit int) (dispatch.FanOutResult, error)
	DeliverDue(ctx context.Context, input dispatch.DeliverInput) (dispatch.DeliverResult, error)
}

type DispatchServiceMetrics struct {
	next     dispatchService
	registry *Registry
}

func NewDispatchServiceMetrics(next dispatchService, registry *Registry) *DispatchServiceMetrics {
	return &DispatchServiceMetrics{next: next, registry: registry}
}

func (m *DispatchServiceMetrics) FanOut(ctx context.Context, limit int) (dispatch.FanOutResult, error) {
	output, err := m.next.FanOut(ctx, limit)
	m.registry.published.Add(float64(output.Events))
	return output, err
}

func (m *DispatchServiceMetrics) DeliverDue(ctx context.Context, input dispatch.DeliverInput) (dispatch.DeliverResult, error) {
	output, err := m.next.DeliverDue(ctx, input)
	m.registry.deliveries.WithLabelValues("succeeded").Add(float64(output.Succeeded))
	m.registry.deliveries.WithLabelValues("retry").Add(float64(output.Retried))
	m.registry.deliveries.WithLabelValues("dead").Add(float64(output.Dead))
	return output, err
}
