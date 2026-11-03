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

func (l RequestLogger) LogRequest(ctx context.Context, method, route string) func(status int, duration time.Duration, err error) {
	return func(status int, duration time.Duration, err error) {
		attrs := []slog.Attr{
			slog.String("method", method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int64("duration_ms", duration.Milliseconds()),
		}
		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
		}
		l.logger.LogAttrs(ctx, statusLevel(status), "http.request", attrs...)
	}
}

func statusLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func (l RequestLogger) ContextWithRequestID(ctx context.Context, id string) context.Context {
	return WithRequestID(ctx, id)
}

func (l RequestLogger) ReportPanic(ctx context.Context, recovered any, stack []byte) {
	l.logger.ErrorContext(ctx, "http.panic",
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(stack)))
}

func (l RequestLogger) Report(ctx context.Context, task string, err error) {
	l.logger.ErrorContext(ctx, "poller.task_failed",
		slog.String("task", task),
		slog.String("error", err.Error()))
}
