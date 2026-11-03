package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type QuerySpans interface {
	StartQuerySpan(ctx context.Context, sql string) (context.Context, func(err error))
}

type queryEndKey struct{}

type queryTracer struct {
	spans QuerySpans
}

func (t queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, end := t.spans.StartQuerySpan(ctx, data.SQL)
	return context.WithValue(ctx, queryEndKey{}, end)
}

func (t queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if end, ok := ctx.Value(queryEndKey{}).(func(error)); ok {
		end(data.Err)
	}
}
