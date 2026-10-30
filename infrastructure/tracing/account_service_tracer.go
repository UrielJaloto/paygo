package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/services/account"
)

type accountService interface {
	Open(ctx context.Context, input account.OpenInput) (account.AccountOutput, error)
	Get(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error)
	List(ctx context.Context, input account.ListInput) (account.AccountPage, error)
	Balance(ctx context.Context, query account.AccountQuery) (account.BalanceOutput, error)
	Statement(ctx context.Context, input account.PageInput) (account.StatementPage, error)
	Freeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error)
	Unfreeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error)
}

type AccountServiceTracer struct {
	next   accountService
	tracer trace.Tracer
}

func NewAccountServiceTracer(next accountService, tracer trace.Tracer) *AccountServiceTracer {
	return &AccountServiceTracer{next: next, tracer: tracer}
}

func (t *AccountServiceTracer) Open(ctx context.Context, input account.OpenInput) (account.AccountOutput, error) {
	ctx, span := t.tracer.Start(ctx, "account.Open")
	output, err := t.next.Open(ctx, input)
	finish(span, err)
	return output, err
}

func (t *AccountServiceTracer) Get(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error) {
	ctx, span := t.tracer.Start(ctx, "account.Get")
	output, err := t.next.Get(ctx, query)
	finish(span, err)
	return output, err
}

func (t *AccountServiceTracer) List(ctx context.Context, input account.ListInput) (account.AccountPage, error) {
	ctx, span := t.tracer.Start(ctx, "account.List")
	output, err := t.next.List(ctx, input)
	finish(span, err)
	return output, err
}

func (t *AccountServiceTracer) Balance(ctx context.Context, query account.AccountQuery) (account.BalanceOutput, error) {
	ctx, span := t.tracer.Start(ctx, "account.Balance")
	output, err := t.next.Balance(ctx, query)
	finish(span, err)
	return output, err
}

func (t *AccountServiceTracer) Statement(ctx context.Context, input account.PageInput) (account.StatementPage, error) {
	ctx, span := t.tracer.Start(ctx, "account.Statement")
	output, err := t.next.Statement(ctx, input)
	finish(span, err)
	return output, err
}

func (t *AccountServiceTracer) Freeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error) {
	ctx, span := t.tracer.Start(ctx, "account.Freeze")
	output, err := t.next.Freeze(ctx, query)
	finish(span, err)
	return output, err
}

func (t *AccountServiceTracer) Unfreeze(ctx context.Context, query account.AccountQuery) (account.AccountOutput, error) {
	ctx, span := t.tracer.Start(ctx, "account.Unfreeze")
	output, err := t.next.Unfreeze(ctx, query)
	finish(span, err)
	return output, err
}
