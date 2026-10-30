package logging

import (
	"context"
	"io"
	"log/slog"
)

type TraceIDReader interface {
	TraceID(ctx context.Context) string
}

type requestIDKey struct{}

type untraced struct{}

func (untraced) TraceID(context.Context) string {
	return ""
}

func NewJSONLogger(w io.Writer, level slog.Level) *slog.Logger {
	return NewTracedJSONLogger(w, level, untraced{})
}

func NewTracedJSONLogger(w io.Writer, level slog.Level, traces TraceIDReader) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
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
	if id := h.traces.TraceID(ctx); id != "" {
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
