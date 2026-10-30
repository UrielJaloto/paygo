package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/services/transfer"
)

type transferService interface {
	Transfer(ctx context.Context, input transfer.TransferInput) (transfer.TransferOutput, error)
	Deposit(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error)
	Withdraw(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error)
	Reverse(ctx context.Context, input transfer.ReverseInput) (transfer.TransferOutput, error)
	Get(ctx context.Context, query transfer.TransferQuery) (transfer.TransferOutput, error)
	List(ctx context.Context, input transfer.ListInput) (transfer.TransferPage, error)
}

type TransferServiceTracer struct {
	next   transferService
	tracer trace.Tracer
}

func NewTransferServiceTracer(next transferService, tracer trace.Tracer) *TransferServiceTracer {
	return &TransferServiceTracer{next: next, tracer: tracer}
}

func (t *TransferServiceTracer) Transfer(ctx context.Context, input transfer.TransferInput) (transfer.TransferOutput, error) {
	ctx, span := t.tracer.Start(ctx, "transfer.Transfer")
	output, err := t.next.Transfer(ctx, input)
	finish(span, err)
	return output, err
}

func (t *TransferServiceTracer) Deposit(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error) {
	ctx, span := t.tracer.Start(ctx, "transfer.Deposit")
	output, err := t.next.Deposit(ctx, input)
	finish(span, err)
	return output, err
}

func (t *TransferServiceTracer) Withdraw(ctx context.Context, input transfer.FundingInput) (transfer.TransferOutput, error) {
	ctx, span := t.tracer.Start(ctx, "transfer.Withdraw")
	output, err := t.next.Withdraw(ctx, input)
	finish(span, err)
	return output, err
}

func (t *TransferServiceTracer) Reverse(ctx context.Context, input transfer.ReverseInput) (transfer.TransferOutput, error) {
	ctx, span := t.tracer.Start(ctx, "transfer.Reverse")
	output, err := t.next.Reverse(ctx, input)
	finish(span, err)
	return output, err
}

func (t *TransferServiceTracer) Get(ctx context.Context, query transfer.TransferQuery) (transfer.TransferOutput, error) {
	ctx, span := t.tracer.Start(ctx, "transfer.Get")
	output, err := t.next.Get(ctx, query)
	finish(span, err)
	return output, err
}

func (t *TransferServiceTracer) List(ctx context.Context, input transfer.ListInput) (transfer.TransferPage, error) {
	ctx, span := t.tracer.Start(ctx, "transfer.List")
	output, err := t.next.List(ctx, input)
	finish(span, err)
	return output, err
}
