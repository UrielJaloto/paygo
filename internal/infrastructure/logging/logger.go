package logging

import (
	"context"
	"io"
	"log/slog"
)

type TraceIDReader interface {
	ReadTraceID(ctx context.Context) string
}

type requestIDKey struct{}

func NewJSONLogger(writer io.Writer, level slog.Level) *slog.Logger {
	return NewTracedJSONLogger(writer, level, nil)
}

func NewTracedJSONLogger(writer io.Writer, level slog.Level, traces TraceIDReader) *slog.Logger {
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})
	return slog.New(contextHandler{Handler: handler, traces: traces})
}

func AttachRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func ReadRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type contextHandler struct {
	slog.Handler
	traces TraceIDReader
}

func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := ReadRequestID(ctx); id != "" {
		record.AddAttrs(slog.String("request_id", id))
	}
	if id := h.readTraceID(ctx); id != "" {
		record.AddAttrs(slog.String("trace_id", id))
	}
	return h.Handler.Handle(ctx, record)
}

func (h contextHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attributes), traces: h.traces}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name), traces: h.traces}
}

func (h contextHandler) readTraceID(ctx context.Context) string {
	if h.traces == nil {
		return ""
	}
	return h.traces.ReadTraceID(ctx)
}
