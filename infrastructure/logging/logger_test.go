package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/logging"
)

var errBoom = errors.New("boom")

type stubTraces struct {
	id string
}

func (s stubTraces) TraceID(context.Context) string {
	return s.id
}

func decodeLines(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("decoding %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}

func singleLine(t *testing.T, buffer *bytes.Buffer) map[string]any {
	t.Helper()
	lines := decodeLines(t, buffer)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1: %s", len(lines), buffer.String())
	}
	return lines[0]
}

func assertAttrs(t *testing.T, line map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s = %v, want %v", key, line[key], value)
		}
	}
}

func assertPresent(t *testing.T, line map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := line[key]; !ok {
			t.Errorf("%s missing from %v", key, line)
		}
	}
}

func TestJSONLogger_Fields_ContainTimeLevelMessage(t *testing.T) {
	var buffer bytes.Buffer
	logging.NewJSONLogger(&buffer, slog.LevelInfo).Info("hello")
	line := singleLine(t, &buffer)
	assertPresent(t, line, "time")
	assertAttrs(t, line, map[string]any{"level": "INFO", "msg": "hello"})
}

func TestJSONLogger_RequestID_AddedFromContext(t *testing.T) {
	var buffer bytes.Buffer
	ctx := logging.WithRequestID(context.Background(), "req-1")
	logging.NewJSONLogger(&buffer, slog.LevelInfo).InfoContext(ctx, "hello")
	assertAttrs(t, singleLine(t, &buffer), map[string]any{"request_id": "req-1"})
}

func TestJSONLogger_NoRequestID_AttrAbsent(t *testing.T) {
	var buffer bytes.Buffer
	logging.NewJSONLogger(&buffer, slog.LevelInfo).Info("hello")
	if _, ok := singleLine(t, &buffer)["request_id"]; ok {
		t.Error("request_id present without a request id")
	}
}

func TestTracedJSONLogger_TraceID_PresentOnlyWhenReaderReturnsIt(t *testing.T) {
	tests := []struct {
		name    string
		traceID string
		present bool
	}{
		{name: "trace id", traceID: "abc", present: true},
		{name: "empty", traceID: "", present: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buffer bytes.Buffer
			logging.NewTracedJSONLogger(&buffer, slog.LevelInfo, stubTraces{id: tt.traceID}).Info("hello")
			value, ok := singleLine(t, &buffer)["trace_id"]
			if ok != tt.present {
				t.Fatalf("trace_id present = %v, want %v", ok, tt.present)
			}
			if ok && value != tt.traceID {
				t.Errorf("trace_id = %v, want %s", value, tt.traceID)
			}
		})
	}
}

func TestJSONLogger_DerivedLoggers_KeepContextAttrs(t *testing.T) {
	var buffer bytes.Buffer
	ctx := logging.WithRequestID(context.Background(), "req-1")
	logger := logging.NewTracedJSONLogger(&buffer, slog.LevelInfo, stubTraces{id: "abc"})
	logger.With("component", "api").WithGroup("detail").InfoContext(ctx, "hello", "key", "value")
	line := singleLine(t, &buffer)
	assertAttrs(t, line, map[string]any{"component": "api"})
	detail, ok := line["detail"].(map[string]any)
	if !ok {
		t.Fatalf("detail group missing from %v", line)
	}
	assertAttrs(t, detail, map[string]any{"key": "value", "request_id": "req-1", "trace_id": "abc"})
}

func TestJSONLogger_LevelWarn_HidesDebug(t *testing.T) {
	var buffer bytes.Buffer
	logger := logging.NewJSONLogger(&buffer, slog.LevelWarn)
	logger.Debug("hidden")
	logger.Warn("shown")
	assertAttrs(t, singleLine(t, &buffer), map[string]any{"msg": "shown"})
}

func TestRequestID_MissingFromContext_ReturnsEmpty(t *testing.T) {
	if id := logging.RequestID(context.Background()); id != "" {
		t.Errorf("RequestID = %q, want empty", id)
	}
}

func TestRequestLogger_LogRequest_WritesAccessLog(t *testing.T) {
	var buffer bytes.Buffer
	requests := logging.NewRequestLogger(logging.NewJSONLogger(&buffer, slog.LevelInfo))
	ctx := requests.ContextWithRequestID(context.Background(), "req-1")
	requests.LogRequest(ctx, "GET", "/v1/accounts/:id")(200, 1500*time.Millisecond, nil)
	line := singleLine(t, &buffer)
	assertAttrs(t, line, map[string]any{
		"level":       "INFO",
		"method":      "GET",
		"route":       "/v1/accounts/:id",
		"status":      float64(200),
		"duration_ms": float64(1500),
		"request_id":  "req-1",
	})
	if _, ok := line["error"]; ok {
		t.Errorf("error attribute present on success: %v", line)
	}
}

func TestRequestLogger_LogRequest_FailureLevelFollowsStatus(t *testing.T) {
	cases := []struct {
		status int
		level  string
	}{
		{status: 422, level: "WARN"},
		{status: 500, level: "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			var buffer bytes.Buffer
			requests := logging.NewRequestLogger(logging.NewJSONLogger(&buffer, slog.LevelInfo))
			requests.LogRequest(context.Background(), "POST", "/v1/transfers")(tc.status, time.Millisecond, errBoom)
			assertAttrs(t, singleLine(t, &buffer), map[string]any{"level": tc.level, "status": float64(tc.status), "error": "boom"})
		})
	}
}

func TestRequestLogger_ReportPanic_WritesErrorWithStack(t *testing.T) {
	var buffer bytes.Buffer
	requests := logging.NewRequestLogger(logging.NewJSONLogger(&buffer, slog.LevelInfo))
	requests.ReportPanic(context.Background(), "kaboom", []byte("stack trace"))
	assertAttrs(t, singleLine(t, &buffer), map[string]any{"level": "ERROR", "panic": "kaboom", "stack": "stack trace"})
}

func TestRequestLogger_Report_WritesTaskAndError(t *testing.T) {
	var buffer bytes.Buffer
	requests := logging.NewRequestLogger(logging.NewJSONLogger(&buffer, slog.LevelInfo))
	requests.Report(context.Background(), "fan-out", errBoom)
	assertAttrs(t, singleLine(t, &buffer), map[string]any{"level": "ERROR", "task": "fan-out", "error": "boom"})
}
