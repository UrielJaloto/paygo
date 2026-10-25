package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/UrielJaloto/paygo/infrastructure/postgres"
)

type spySpans struct {
	sql   string
	ended bool
	err   error
}

func (s *spySpans) StartQuerySpan(ctx context.Context, sql string) (context.Context, func(error)) {
	s.sql = sql
	return ctx, func(err error) { s.ended, s.err = true, err }
}

func TestQueryTracer_StartAndEnd_ReportStatementAndError(t *testing.T) {
	spans := &spySpans{}
	tracer := postgres.NewQueryTracer(spans)
	failure := errors.NewMoney("boom")
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: failure})
	if spans.sql != "SELECT 1" || !spans.ended || !errors.Is(spans.err, failure) {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestQueryTracer_EndWithoutStart_IsIgnored(t *testing.T) {
	spans := &spySpans{}
	postgres.NewQueryTracer(spans).TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
	if spans.ended {
		t.Fatal("span ended without a start")
	}
}
