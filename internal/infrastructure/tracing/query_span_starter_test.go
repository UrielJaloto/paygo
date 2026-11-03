package tracing_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/UrielJaloto/paygo/internal/infrastructure/tracing"
)

func TestQuerySpanStarter_Success_RecordsClientSpanWithStatement(t *testing.T) {
	exporter, tracer := newRecorder(t)
	_, end := tracing.NewQuerySpanStarter(tracer).StartQuerySpan(context.Background(), "SELECT 1")
	end(nil)
	span := singleSpan(t, exporter)
	if span.Name != "postgres.query" || span.Status.Code == codes.Error {
		t.Errorf("span = %s, status %v", span.Name, span.Status.Code)
	}
	want := attribute.String("db.query.text", "SELECT 1")
	found := false
	for _, attr := range span.Attributes {
		found = found || attr == want
	}
	if !found {
		t.Errorf("attributes = %v, want %v", span.Attributes, want)
	}
}

func TestQuerySpanStarter_Error_MarksSpanFailed(t *testing.T) {
	exporter, tracer := newRecorder(t)
	_, end := tracing.NewQuerySpanStarter(tracer).StartQuerySpan(context.Background(), "SELECT 1")
	end(errBoom)
	if span := singleSpan(t, exporter); span.Status.Code != codes.Error {
		t.Errorf("status = %v, want error", span.Status.Code)
	}
}
