package logging_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	domainaccount "github.com/UrielJaloto/paygo/domain/account"
	domainapikey "github.com/UrielJaloto/paygo/domain/apikey"
	domaintransfer "github.com/UrielJaloto/paygo/domain/transfer"
	domainwebhook "github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/logging"
	"github.com/UrielJaloto/paygo/services/account"
	"github.com/UrielJaloto/paygo/services/apikey"
	"github.com/UrielJaloto/paygo/services/dispatch"
	"github.com/UrielJaloto/paygo/services/transfer"
	"github.com/UrielJaloto/paygo/services/webhook"
)

const (
	issuedToken = "secret-issued-token"
	inputToken  = "secret-input-token"
	hookSecret  = "secret-webhook-value"
)

type methodCase[D any] struct {
	msg  string
	want any
	call func(ctx context.Context, decorator D) (any, error)
}

type decoratorSuite[D any] struct {
	business error
	build    func(err error, logger *slog.Logger) D
	cases    []methodCase[D]
}

func (s decoratorSuite[D]) run(t *testing.T) {
	t.Helper()
	scenarios := []struct {
		name  string
		err   error
		level string
	}{
		{name: "success", err: nil, level: "DEBUG"},
		{name: "business error", err: fmt.Errorf("wrapped: %w", s.business), level: "WARN"},
		{name: "unexpected error", err: errBoom, level: "ERROR"},
	}
	for _, method := range s.cases {
		for _, scenario := range scenarios {
			t.Run(method.msg+"/"+scenario.name, func(t *testing.T) {
				var buffer bytes.Buffer
				decorator := s.build(scenario.err, logging.NewJSONLogger(&buffer, slog.LevelDebug))
				got, err := method.call(context.Background(), decorator)
				if !errors.Is(err, scenario.err) {
					t.Fatalf("err = %v, want %v", err, scenario.err)
				}
				if !reflect.DeepEqual(got, method.want) {
					t.Errorf("output = %+v, want %+v", got, method.want)
				}
				assertOperationLine(t, singleLine(t, &buffer), method.msg, scenario.level, scenario.err)
			})
		}
	}
}

func assertOperationLine(t *testing.T, line map[string]any, msg, level string, err error) {
	t.Helper()
	assertPresent(t, line, "duration_ms")
	assertAttrs(t, line, map[string]any{"msg": msg, "level": level})
	if err == nil {
		return
	}
	assertAttrs(t, line, map[string]any{"error": err.Error()})
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

func TestAccountServiceLogger_EveryMethod_LogsAndStaysTransparent(t *testing.T) {
	output := account.AccountOutput{ID: "acc-1"}
	accountPage := account.AccountPage{NextCursor: "next"}
	balance := account.BalanceOutput{AccountID: "acc-1", AmountMinor: 10}
	lines := account.StatementPage{NextCursor: "lines"}
	query := account.AccountQuery{OwnerID: "own-1", AccountID: "acc-1"}
	decoratorSuite[*logging.AccountServiceLogger]{
		business: domainaccount.ErrAccountNotFound,
		build: func(err error, logger *slog.Logger) *logging.AccountServiceLogger {
			next := stubAccounts{output: output, page: accountPage, balance: balance, lines: lines, err: err}
			return logging.NewAccountServiceLogger(next, logger)
		},
		cases: []methodCase[*logging.AccountServiceLogger]{
			{msg: "account.Open", want: output, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.Open(ctx, account.OpenInput{OwnerID: "own-1"})
			}},
			{msg: "account.Get", want: output, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.Get(ctx, query)
			}},
			{msg: "account.List", want: accountPage, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.List(ctx, account.ListInput{OwnerID: "own-1"})
			}},
			{msg: "account.Balance", want: balance, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.Balance(ctx, query)
			}},
			{msg: "account.Statement", want: lines, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.Statement(ctx, account.PageInput{OwnerID: "own-1", AccountID: "acc-1"})
			}},
			{msg: "account.Freeze", want: output, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.Freeze(ctx, query)
			}},
			{msg: "account.Unfreeze", want: output, call: func(ctx context.Context, d *logging.AccountServiceLogger) (any, error) {
				return d.Unfreeze(ctx, query)
			}},
		},
	}.run(t)
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

func TestTransferServiceLogger_EveryMethod_LogsAndStaysTransparent(t *testing.T) {
	output := transfer.TransferOutput{ID: "trf-1"}
	transferPage := transfer.TransferPage{NextCursor: "next"}
	funding := transfer.FundingInput{OwnerID: "own-1", AccountID: "acc-1", Amount: "10.00"}
	decoratorSuite[*logging.TransferServiceLogger]{
		business: domaintransfer.ErrInsufficientFunds,
		build: func(err error, logger *slog.Logger) *logging.TransferServiceLogger {
			return logging.NewTransferServiceLogger(stubTransfers{output: output, page: transferPage, err: err}, logger)
		},
		cases: []methodCase[*logging.TransferServiceLogger]{
			{msg: "transfer.Transfer", want: output, call: func(ctx context.Context, d *logging.TransferServiceLogger) (any, error) {
				return d.Transfer(ctx, transfer.TransferInput{OwnerID: "own-1"})
			}},
			{msg: "transfer.Deposit", want: output, call: func(ctx context.Context, d *logging.TransferServiceLogger) (any, error) {
				return d.Deposit(ctx, funding)
			}},
			{msg: "transfer.Withdraw", want: output, call: func(ctx context.Context, d *logging.TransferServiceLogger) (any, error) {
				return d.Withdraw(ctx, funding)
			}},
			{msg: "transfer.Reverse", want: output, call: func(ctx context.Context, d *logging.TransferServiceLogger) (any, error) {
				return d.Reverse(ctx, transfer.ReverseInput{TransferID: "trf-1"})
			}},
			{msg: "transfer.Get", want: output, call: func(ctx context.Context, d *logging.TransferServiceLogger) (any, error) {
				return d.Get(ctx, transfer.TransferQuery{TransferID: "trf-1"})
			}},
			{msg: "transfer.List", want: transferPage, call: func(ctx context.Context, d *logging.TransferServiceLogger) (any, error) {
				return d.List(ctx, transfer.ListInput{OwnerID: "own-1"})
			}},
		},
	}.run(t)
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

func TestAPIKeyServiceLogger_EveryMethod_LogsAndStaysTransparent(t *testing.T) {
	issued := apikey.IssuedKey{ID: "key-1", Token: issuedToken}
	principal := apikey.Principal{OwnerID: "own-1"}
	decoratorSuite[*logging.APIKeyServiceLogger]{
		business: domainapikey.ErrInvalidToken,
		build: func(err error, logger *slog.Logger) *logging.APIKeyServiceLogger {
			return logging.NewAPIKeyServiceLogger(stubAPIKeys{issued: issued, principal: principal, err: err}, logger)
		},
		cases: []methodCase[*logging.APIKeyServiceLogger]{
			{msg: "apikey.Issue", want: issued, call: func(ctx context.Context, d *logging.APIKeyServiceLogger) (any, error) {
				return d.Issue(ctx, apikey.IssueInput{OwnerID: "own-1"})
			}},
			{msg: "apikey.Authenticate", want: principal, call: func(ctx context.Context, d *logging.APIKeyServiceLogger) (any, error) {
				return d.Authenticate(ctx, inputToken)
			}},
			{msg: "apikey.Revoke", want: nil, call: func(ctx context.Context, d *logging.APIKeyServiceLogger) (any, error) {
				return nil, d.Revoke(ctx, apikey.RevokeInput{KeyID: "key-1"})
			}},
		},
	}.run(t)
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

func TestWebhookServiceLogger_EveryMethod_LogsAndStaysTransparent(t *testing.T) {
	registered := webhook.RegisteredEndpoint{Secret: hookSecret}
	endpoints := []webhook.EndpointOutput{{ID: "end-1"}}
	deliveries := webhook.DeliveryPage{NextCursor: "next"}
	delivery := webhook.DeliveryOutput{ID: "del-1"}
	decoratorSuite[*logging.WebhookServiceLogger]{
		business: domainwebhook.ErrEndpointNotFound,
		build: func(err error, logger *slog.Logger) *logging.WebhookServiceLogger {
			next := stubWebhooks{registered: registered, endpoints: endpoints, page: deliveries, delivery: delivery, err: err}
			return logging.NewWebhookServiceLogger(next, logger)
		},
		cases: []methodCase[*logging.WebhookServiceLogger]{
			{msg: "webhook.Register", want: registered, call: func(ctx context.Context, d *logging.WebhookServiceLogger) (any, error) {
				return d.Register(ctx, webhook.RegisterInput{OwnerID: "own-1"})
			}},
			{msg: "webhook.ListEndpoints", want: endpoints, call: func(ctx context.Context, d *logging.WebhookServiceLogger) (any, error) {
				return d.ListEndpoints(ctx, "own-1")
			}},
			{msg: "webhook.Disable", want: nil, call: func(ctx context.Context, d *logging.WebhookServiceLogger) (any, error) {
				return nil, d.Disable(ctx, webhook.EndpointQuery{EndpointID: "end-1"})
			}},
			{msg: "webhook.ListDeliveries", want: deliveries, call: func(ctx context.Context, d *logging.WebhookServiceLogger) (any, error) {
				return d.ListDeliveries(ctx, webhook.DeliveryListInput{OwnerID: "own-1"})
			}},
			{msg: "webhook.Replay", want: delivery, call: func(ctx context.Context, d *logging.WebhookServiceLogger) (any, error) {
				return d.Replay(ctx, webhook.DeliveryQuery{DeliveryID: "del-1"})
			}},
		},
	}.run(t)
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

func TestDispatchServiceLogger_EveryMethod_LogsAndStaysTransparent(t *testing.T) {
	fanOut := dispatch.FanOutResult{Events: 2, Deliveries: 3}
	deliver := dispatch.DeliverResult{Succeeded: 2, Retried: 1, Dead: 1}
	decoratorSuite[*logging.DispatchServiceLogger]{
		business: domainwebhook.ErrDeliveryNotPending,
		build: func(err error, logger *slog.Logger) *logging.DispatchServiceLogger {
			return logging.NewDispatchServiceLogger(stubDispatch{fanOut: fanOut, deliver: deliver, err: err}, logger)
		},
		cases: []methodCase[*logging.DispatchServiceLogger]{
			{msg: "dispatch.FanOut", want: fanOut, call: func(ctx context.Context, d *logging.DispatchServiceLogger) (any, error) {
				return d.FanOut(ctx, 10)
			}},
			{msg: "dispatch.DeliverDue", want: deliver, call: func(ctx context.Context, d *logging.DispatchServiceLogger) (any, error) {
				return d.DeliverDue(ctx, dispatch.DeliverInput{Limit: 10})
			}},
		},
	}.run(t)
}

func TestServiceLoggers_Secrets_NeverLogged(t *testing.T) {
	var buffer bytes.Buffer
	logger := logging.NewJSONLogger(&buffer, slog.LevelDebug)
	ctx := context.Background()
	keys := logging.NewAPIKeyServiceLogger(stubAPIKeys{issued: apikey.IssuedKey{Token: issuedToken}}, logger)
	hooks := logging.NewWebhookServiceLogger(stubWebhooks{registered: webhook.RegisteredEndpoint{Secret: hookSecret}}, logger)
	if _, err := keys.Issue(ctx, apikey.IssueInput{OwnerID: "own-1"}); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := keys.Authenticate(ctx, inputToken); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if _, err := hooks.Register(ctx, webhook.RegisterInput{OwnerID: "own-1"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	for _, secret := range []string{issuedToken, inputToken, hookSecret} {
		if strings.Contains(buffer.String(), secret) {
			t.Errorf("log contains %q: %s", secret, buffer.String())
		}
	}
}
