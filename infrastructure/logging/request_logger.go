package logging

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type RequestLogger struct {
	logger *slog.Logger
}

func NewRequestLogger(logger *slog.Logger) RequestLogger {
	return RequestLogger{logger: logger}
}

func (l RequestLogger) LogRequest(ctx context.Context, method, route string) func(status int, duration time.Duration) {
	return func(status int, duration time.Duration) {
		l.logger.InfoContext(ctx, "http.request",
			slog.String("method", method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int64("duration_ms", duration.Milliseconds()))
	}
}

func (l RequestLogger) AttachRequestID(ctx context.Context, id string) context.Context {
	return AttachRequestID(ctx, id)
}

func (l RequestLogger) ReportPanic(ctx context.Context, recovered any, stack []byte) {
	l.logger.ErrorContext(ctx, "http.panic",
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(stack)))
}

func (l RequestLogger) ReportTaskError(ctx context.Context, task string, err error) {
	l.logger.ErrorContext(ctx, "poller.task_failed",
		slog.String("task", task),
		slog.String("error", err.Error()))
}
