package logging

import (
	"context"
	"log/slog"

	"github.com/UrielJaloto/paygo/services/dispatch"
)

type dispatchService interface {
	FanOut(ctx context.Context, limit int) (dispatch.FanOutResult, error)
	DeliverDue(ctx context.Context, input dispatch.DeliverInput) (dispatch.DeliverResult, error)
}

type DispatchServiceLogger struct {
	next   dispatchService
	logger *slog.Logger
}

func NewDispatchServiceLogger(next dispatchService, logger *slog.Logger) *DispatchServiceLogger {
	return &DispatchServiceLogger{next: next, logger: logger}
}

func (l *DispatchServiceLogger) FanOut(ctx context.Context, limit int) (dispatch.FanOutResult, error) {
	op := begin(l.logger, "dispatch.FanOut")
	output, err := l.next.FanOut(ctx, limit)
	op.finish(ctx, err, slog.Int("limit", limit))
	return output, err
}

func (l *DispatchServiceLogger) DeliverDue(ctx context.Context, input dispatch.DeliverInput) (dispatch.DeliverResult, error) {
	op := begin(l.logger, "dispatch.DeliverDue")
	output, err := l.next.DeliverDue(ctx, input)
	op.finish(ctx, err, slog.Int("limit", input.Limit))
	return output, err
}
