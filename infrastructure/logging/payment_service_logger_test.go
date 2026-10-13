package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	domainpayment "github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/infrastructure/logging"
	"github.com/UrielJaloto/paygo/services/ledgercheck"
	"github.com/UrielJaloto/paygo/services/payment"
)

const (
	payerDocument = "52998224725"
	pixKey        = "ana@example.com"
)

type stubPayments struct {
	charge payment.ChargeOutput
	payout payment.PayoutOutput
	sync   payment.SyncResult
	err    error
}

func (s stubPayments) CreateCharge(context.Context, payment.ChargeInput) (payment.ChargeOutput, error) {
	return s.charge, s.err
}

func (s stubPayments) GetCharge(context.Context, payment.ChargeQuery) (payment.ChargeOutput, error) {
	return s.charge, s.err
}

func (s stubPayments) CreatePayout(context.Context, payment.PayoutInput) (payment.PayoutOutput, error) {
	return s.payout, s.err
}

func (s stubPayments) GetPayout(context.Context, payment.PayoutQuery) (payment.PayoutOutput, error) {
	return s.payout, s.err
}

func (s stubPayments) HandleWebhook(context.Context, payment.WebhookInput) error {
	return s.err
}

func (s stubPayments) Sync(context.Context, int) (payment.SyncResult, error) {
	return s.sync, s.err
}

func TestPaymentServiceLogger_EveryMethod_LogsAndStaysTransparent(t *testing.T) {
	charge := payment.ChargeOutput{ID: "charge-1"}
	payout := payment.PayoutOutput{ID: "payout-1"}
	synced := payment.SyncResult{Checked: 1}
	decoratorSuite[*logging.PaymentServiceLogger]{
		business: domainpayment.ErrProviderRejected,
		build: func(err error, logger *slog.Logger) *logging.PaymentServiceLogger {
			return logging.NewPaymentServiceLogger(stubPayments{charge: charge, payout: payout, sync: synced, err: err}, logger)
		},
		cases: []methodCase[*logging.PaymentServiceLogger]{
			{msg: "payment.CreateCharge", want: charge, call: func(ctx context.Context, d *logging.PaymentServiceLogger) (any, error) {
				return d.CreateCharge(ctx, payment.ChargeInput{})
			}},
			{msg: "payment.GetCharge", want: charge, call: func(ctx context.Context, d *logging.PaymentServiceLogger) (any, error) {
				return d.GetCharge(ctx, payment.ChargeQuery{})
			}},
			{msg: "payment.CreatePayout", want: payout, call: func(ctx context.Context, d *logging.PaymentServiceLogger) (any, error) {
				return d.CreatePayout(ctx, payment.PayoutInput{})
			}},
			{msg: "payment.GetPayout", want: payout, call: func(ctx context.Context, d *logging.PaymentServiceLogger) (any, error) {
				return d.GetPayout(ctx, payment.PayoutQuery{})
			}},
			{msg: "payment.HandleWebhook", want: nil, call: func(ctx context.Context, d *logging.PaymentServiceLogger) (any, error) {
				return nil, d.HandleWebhook(ctx, payment.WebhookInput{})
			}},
			{msg: "payment.Sync", want: synced, call: func(ctx context.Context, d *logging.PaymentServiceLogger) (any, error) {
				return d.Sync(ctx, 1)
			}},
		},
	}.run(t)
}

func TestPaymentServiceLogger_SensitiveFields_KeepOnlyLastFour(t *testing.T) {
	tests := []struct {
		name   string
		call   func(d *logging.PaymentServiceLogger) error
		attribute   string
		want   string
		secret string
	}{
		{
			name: "payer document",
			call: func(d *logging.PaymentServiceLogger) error {
				_, err := d.CreateCharge(context.Background(), payment.ChargeInput{PayerDocument: payerDocument})
				return err
			},
			attribute: "payer_document", want: "4725", secret: payerDocument,
		},
		{
			name: "pix key",
			call: func(d *logging.PaymentServiceLogger) error {
				_, err := d.CreatePayout(context.Background(), payment.PayoutInput{PixKey: pixKey})
				return err
			},
			attribute: "pix_key", want: ".com", secret: pixKey,
		},
		{
			name: "short value stays whole",
			call: func(d *logging.PaymentServiceLogger) error {
				_, err := d.CreatePayout(context.Background(), payment.PayoutInput{PixKey: "abc"})
				return err
			},
			attribute: "pix_key", want: "abc", secret: payerDocument,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var buffer bytes.Buffer
			decorator := logging.NewPaymentServiceLogger(stubPayments{}, logging.NewJSONLogger(&buffer, slog.LevelDebug))
			if err := testCase.call(decorator); err != nil {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(buffer.String(), testCase.secret) {
				t.Errorf("log carries the full value: %s", buffer.String())
			}
			assertAttributes(t, singleLine(t, &buffer), map[string]any{testCase.attribute: testCase.want})
		})
	}
}

type stubLedgerCheck struct {
	output ledgercheck.CheckOutput
	err    error
}

func (s stubLedgerCheck) Check(context.Context) (ledgercheck.CheckOutput, error) {
	return s.output, s.err
}

func TestLedgerCheckServiceLogger_Check_LogsAndStaysTransparent(t *testing.T) {
	output := ledgercheck.CheckOutput{OK: true, Accounts: 2}
	decoratorSuite[*logging.LedgerCheckServiceLogger]{
		business: domainpayment.ErrProviderUnavailable,
		build: func(err error, logger *slog.Logger) *logging.LedgerCheckServiceLogger {
			return logging.NewLedgerCheckServiceLogger(stubLedgerCheck{output: output, err: err}, logger)
		},
		cases: []methodCase[*logging.LedgerCheckServiceLogger]{
			{msg: "ledgercheck.Check", want: output, call: func(ctx context.Context, d *logging.LedgerCheckServiceLogger) (any, error) {
				return d.Check(ctx)
			}},
		},
	}.run(t)
}
