package tracing_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/services/account"
	"github.com/UrielJaloto/paygo/services/apikey"
	"github.com/UrielJaloto/paygo/services/dispatch"
	"github.com/UrielJaloto/paygo/services/transfer"
	"github.com/UrielJaloto/paygo/services/webhook"
)

var errBoom = errors.New("boom")

type methodCase[D any] struct {
	span string
	want any
	call func(ctx context.Context, decorator D) (any, error)
}

func runSuite[D any](t *testing.T, build func(err error, tracer trace.Tracer) D, cases []methodCase[D]) {
	t.Helper()
	for _, method := range cases {
		t.Run(method.span+"/success", func(t *testing.T) { assertSpanOnSuccess(t, build, method) })
		t.Run(method.span+"/error", func(t *testing.T) { assertSpanOnError(t, build, method) })
	}
}

func assertSpanOnSuccess[D any](t *testing.T, build func(err error, tracer trace.Tracer) D, method methodCase[D]) {
	t.Helper()
	exporter, tracer := newRecorder(t)
	got, err := method.call(context.Background(), build(nil, tracer))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(got, method.want) {
		t.Errorf("output = %+v, want %+v", got, method.want)
	}
	span := singleSpan(t, exporter)
	if span.Name != method.span || span.Status.Code == codes.Error || len(span.Events) != 0 {
		t.Errorf("span = %s, status %v, events %d", span.Name, span.Status.Code, len(span.Events))
	}
}

func assertSpanOnError[D any](t *testing.T, build func(err error, tracer trace.Tracer) D, method methodCase[D]) {
	t.Helper()
	exporter, tracer := newRecorder(t)
	if _, err := method.call(context.Background(), build(errBoom, tracer)); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want %v", err, errBoom)
	}
	span := singleSpan(t, exporter)
	if span.Status.Code != codes.Error || len(span.Events) != 1 || span.Events[0].Name != "exception" {
		t.Errorf("status = %v, events = %+v", span.Status.Code, span.Events)
	}
}

type stubAccounts struct {
	output  account.AccountOutput
	page    account.AccountPage
	balance account.BalanceOutput
	lines   account.StatementPage
	err     error
}

func (s stubAccounts) Open(context.Context, account.OpenInput) (account.AccountOutput, error) {
	return s.output, s.err
}

func (s stubAccounts) Get(context.Context, account.AccountQuery) (account.AccountOutput, error) {
	return s.output, s.err
}

func (s stubAccounts) List(context.Context, account.ListInput) (account.AccountPage, error) {
	return s.page, s.err
}

func (s stubAccounts) Balance(context.Context, account.AccountQuery) (account.BalanceOutput, error) {
	return s.balance, s.err
}

func (s stubAccounts) Statement(context.Context, account.PageInput) (account.StatementPage, error) {
	return s.lines, s.err
}

func (s stubAccounts) Freeze(context.Context, account.AccountQuery) (account.AccountOutput, error) {
	return s.output, s.err
}

func (s stubAccounts) Unfreeze(context.Context, account.AccountQuery) (account.AccountOutput, error) {
	return s.output, s.err
}

func TestAccountServiceTracer_EveryMethod_RecordsSpan(t *testing.T) {
	output := account.AccountOutput{ID: "acc-1"}
	accountPage := account.AccountPage{NextCursor: "next"}
	balance := account.BalanceOutput{AccountID: "acc-1"}
	lines := account.StatementPage{NextCursor: "lines"}
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.AccountServiceTracer {
		return tracing.NewAccountServiceTracer(stubAccounts{output: output, page: accountPage, balance: balance, lines: lines, err: err}, tracer)
	}, []methodCase[*tracing.AccountServiceTracer]{
		{span: "account.Open", want: output, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.Open(ctx, account.OpenInput{})
		}},
		{span: "account.Get", want: output, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.Get(ctx, account.AccountQuery{})
		}},
		{span: "account.List", want: accountPage, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.List(ctx, account.ListInput{})
		}},
		{span: "account.Balance", want: balance, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.Balance(ctx, account.AccountQuery{})
		}},
		{span: "account.Statement", want: lines, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.Statement(ctx, account.PageInput{})
		}},
		{span: "account.Freeze", want: output, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.Freeze(ctx, account.AccountQuery{})
		}},
		{span: "account.Unfreeze", want: output, call: func(ctx context.Context, d *tracing.AccountServiceTracer) (any, error) {
			return d.Unfreeze(ctx, account.AccountQuery{})
		}},
	})
}

type stubTransfers struct {
	output transfer.TransferOutput
	page   transfer.TransferPage
	err    error
}

func (s stubTransfers) Transfer(context.Context, transfer.TransferInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Deposit(context.Context, transfer.FundingInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Withdraw(context.Context, transfer.FundingInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Reverse(context.Context, transfer.ReverseInput) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) Get(context.Context, transfer.TransferQuery) (transfer.TransferOutput, error) {
	return s.output, s.err
}

func (s stubTransfers) List(context.Context, transfer.ListInput) (transfer.TransferPage, error) {
	return s.page, s.err
}

func TestTransferServiceTracer_EveryMethod_RecordsSpan(t *testing.T) {
	output := transfer.TransferOutput{ID: "trf-1"}
	transferPage := transfer.TransferPage{NextCursor: "next"}
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.TransferServiceTracer {
		return tracing.NewTransferServiceTracer(stubTransfers{output: output, page: transferPage, err: err}, tracer)
	}, []methodCase[*tracing.TransferServiceTracer]{
		{span: "transfer.Transfer", want: output, call: func(ctx context.Context, d *tracing.TransferServiceTracer) (any, error) {
			return d.Transfer(ctx, transfer.TransferInput{})
		}},
		{span: "transfer.Deposit", want: output, call: func(ctx context.Context, d *tracing.TransferServiceTracer) (any, error) {
			return d.Deposit(ctx, transfer.FundingInput{})
		}},
		{span: "transfer.Withdraw", want: output, call: func(ctx context.Context, d *tracing.TransferServiceTracer) (any, error) {
			return d.Withdraw(ctx, transfer.FundingInput{})
		}},
		{span: "transfer.Reverse", want: output, call: func(ctx context.Context, d *tracing.TransferServiceTracer) (any, error) {
			return d.Reverse(ctx, transfer.ReverseInput{})
		}},
		{span: "transfer.Get", want: output, call: func(ctx context.Context, d *tracing.TransferServiceTracer) (any, error) {
			return d.Get(ctx, transfer.TransferQuery{})
		}},
		{span: "transfer.List", want: transferPage, call: func(ctx context.Context, d *tracing.TransferServiceTracer) (any, error) {
			return d.List(ctx, transfer.ListInput{})
		}},
	})
}

type stubAPIKeys struct {
	issued    apikey.IssuedKey
	principal apikey.Principal
	err       error
}

func (s stubAPIKeys) Issue(context.Context, apikey.IssueInput) (apikey.IssuedKey, error) {
	return s.issued, s.err
}

func (s stubAPIKeys) Authenticate(context.Context, string) (apikey.Principal, error) {
	return s.principal, s.err
}

func (s stubAPIKeys) Revoke(context.Context, apikey.RevokeInput) error {
	return s.err
}

func TestAPIKeyServiceTracer_EveryMethod_RecordsSpan(t *testing.T) {
	issued := apikey.IssuedKey{ID: "key-1"}
	principal := apikey.Principal{OwnerID: "own-1"}
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.APIKeyServiceTracer {
		return tracing.NewAPIKeyServiceTracer(stubAPIKeys{issued: issued, principal: principal, err: err}, tracer)
	}, []methodCase[*tracing.APIKeyServiceTracer]{
		{span: "apikey.Issue", want: issued, call: func(ctx context.Context, d *tracing.APIKeyServiceTracer) (any, error) {
			return d.Issue(ctx, apikey.IssueInput{})
		}},
		{span: "apikey.Authenticate", want: principal, call: func(ctx context.Context, d *tracing.APIKeyServiceTracer) (any, error) {
			return d.Authenticate(ctx, "token")
		}},
		{span: "apikey.Revoke", want: nil, call: func(ctx context.Context, d *tracing.APIKeyServiceTracer) (any, error) {
			return nil, d.Revoke(ctx, apikey.RevokeInput{})
		}},
	})
}

type stubWebhooks struct {
	registered webhook.RegisteredEndpoint
	endpoints  []webhook.EndpointOutput
	page       webhook.DeliveryPage
	delivery   webhook.DeliveryOutput
	err        error
}

func (s stubWebhooks) Register(context.Context, webhook.RegisterInput) (webhook.RegisteredEndpoint, error) {
	return s.registered, s.err
}

func (s stubWebhooks) ListEndpoints(context.Context, string) ([]webhook.EndpointOutput, error) {
	return s.endpoints, s.err
}

func (s stubWebhooks) Disable(context.Context, webhook.EndpointQuery) error {
	return s.err
}

func (s stubWebhooks) ListDeliveries(context.Context, webhook.DeliveryListInput) (webhook.DeliveryPage, error) {
	return s.page, s.err
}

func (s stubWebhooks) Replay(context.Context, webhook.DeliveryQuery) (webhook.DeliveryOutput, error) {
	return s.delivery, s.err
}

func TestWebhookServiceTracer_EveryMethod_RecordsSpan(t *testing.T) {
	registered := webhook.RegisteredEndpoint{Secret: "secret"}
	endpoints := []webhook.EndpointOutput{{ID: "end-1"}}
	deliveries := webhook.DeliveryPage{NextCursor: "next"}
	delivery := webhook.DeliveryOutput{ID: "del-1"}
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.WebhookServiceTracer {
		next := stubWebhooks{registered: registered, endpoints: endpoints, page: deliveries, delivery: delivery, err: err}
		return tracing.NewWebhookServiceTracer(next, tracer)
	}, []methodCase[*tracing.WebhookServiceTracer]{
		{span: "webhook.Register", want: registered, call: func(ctx context.Context, d *tracing.WebhookServiceTracer) (any, error) {
			return d.Register(ctx, webhook.RegisterInput{})
		}},
		{span: "webhook.ListEndpoints", want: endpoints, call: func(ctx context.Context, d *tracing.WebhookServiceTracer) (any, error) {
			return d.ListEndpoints(ctx, "own-1")
		}},
		{span: "webhook.Disable", want: nil, call: func(ctx context.Context, d *tracing.WebhookServiceTracer) (any, error) {
			return nil, d.Disable(ctx, webhook.EndpointQuery{})
		}},
		{span: "webhook.ListDeliveries", want: deliveries, call: func(ctx context.Context, d *tracing.WebhookServiceTracer) (any, error) {
			return d.ListDeliveries(ctx, webhook.DeliveryListInput{})
		}},
		{span: "webhook.Replay", want: delivery, call: func(ctx context.Context, d *tracing.WebhookServiceTracer) (any, error) {
			return d.Replay(ctx, webhook.DeliveryQuery{})
		}},
	})
}

type stubDispatch struct {
	fanOut  dispatch.FanOutResult
	deliver dispatch.DeliverResult
	err     error
}

func (s stubDispatch) FanOut(context.Context, int) (dispatch.FanOutResult, error) {
	return s.fanOut, s.err
}

func (s stubDispatch) DeliverDue(context.Context, dispatch.DeliverInput) (dispatch.DeliverResult, error) {
	return s.deliver, s.err
}

func TestDispatchServiceTracer_EveryMethod_RecordsSpan(t *testing.T) {
	fanOut := dispatch.FanOutResult{Events: 1}
	deliver := dispatch.DeliverResult{Succeeded: 1}
	runSuite(t, func(err error, tracer trace.Tracer) *tracing.DispatchServiceTracer {
		return tracing.NewDispatchServiceTracer(stubDispatch{fanOut: fanOut, deliver: deliver, err: err}, tracer)
	}, []methodCase[*tracing.DispatchServiceTracer]{
		{span: "dispatch.FanOut", want: fanOut, call: func(ctx context.Context, d *tracing.DispatchServiceTracer) (any, error) {
			return d.FanOut(ctx, 1)
		}},
		{span: "dispatch.DeliverDue", want: deliver, call: func(ctx context.Context, d *tracing.DispatchServiceTracer) (any, error) {
			return d.DeliverDue(ctx, dispatch.DeliverInput{})
		}},
	})
}
