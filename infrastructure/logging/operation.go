package logging

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/UrielJaloto/paygo/domain/account"
	"github.com/UrielJaloto/paygo/domain/apikey"
	"github.com/UrielJaloto/paygo/domain/ledger"
	"github.com/UrielJaloto/paygo/domain/money"
	"github.com/UrielJaloto/paygo/domain/page"
	"github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/transfer"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/clock"
	apikeyservice "github.com/UrielJaloto/paygo/services/apikey"
)

type operation struct {
	logger  *slog.Logger
	name    string
	started time.Time
}

func begin(logger *slog.Logger, name string) operation {
	return operation{logger: logger, name: name, started: clock.NewMoney().Now()}
}

func (o operation) finish(ctx context.Context, err error, attributes ...slog.Attr) {
	attributes = append(attributes, slog.Int64("duration_ms", clock.NewMoney().Now().Sub(o.started).Milliseconds()))
	level := slog.LevelDebug
	if err != nil {
		level = errorLevel(err)
		attributes = append(attributes, slog.String("error", err.Error()))
	}
	o.logger.LogAttrs(ctx, level, o.name, attributes...)
}

func errorLevel(err error) slog.Level {
	for _, expected := range businessErrors() {
		if errors.Is(err, expected) {
			return slog.LevelWarn
		}
	}
	return slog.LevelError
}

func businessErrors() []error {
	return []error{
		account.ErrInvalidAccountID, account.ErrAccountNotFound, account.ErrAccountFrozen,
		account.ErrAccountAlreadyFrozen, account.ErrAccountNotFrozen, account.ErrConcurrentModification,
		apikeyservice.ErrUnauthenticated, payment.ErrWebhookUnauthorized,
		apikey.ErrInvalidToken, apikey.ErrInvalidOwner, apikey.ErrAPIKeyAlreadyRevoked, apikey.ErrAPIKeyNotFound,
		ledger.ErrTooFewEntries, ledger.ErrUnbalancedTransaction,
		money.ErrUnsupportedCurrency, money.ErrInvalidAmount, money.ErrAmountTooLarge,
		money.ErrCurrencyMismatch, money.ErrAmountOverflow,
		page.ErrInvalidPageLimit, page.ErrInvalidCursor,
		payment.ErrChargeNotFound, payment.ErrPayoutNotFound, payment.ErrInvalidDocument, payment.ErrInvalidPixKey,
		payment.ErrInvalidPayerName, payment.ErrInvalidTransition, payment.ErrUnsupportedProvider,
		payment.ErrProviderRejected, payment.ErrProviderUnavailable,
		transfer.ErrSameAccount, transfer.ErrInsufficientFunds, transfer.ErrTransferNotFound,
		transfer.ErrTransferAlreadyReversed, transfer.ErrReversalNotReversible,
		transfer.ErrIdempotencyConflict, transfer.ErrInvalidIdempotencyKey,
		webhook.ErrDeliveryNotFound, webhook.ErrDeliveryNotReplayable, webhook.ErrDeliveryNotPending,
		webhook.ErrInvalidJitter, webhook.ErrInvalidEndpointURL, webhook.ErrEndpointLimitReached,
		webhook.ErrEndpointNotFound, webhook.ErrEndpointAlreadyDisabled, webhook.ErrInvalidSecret,
		webhook.ErrInvalidRetryPolicy,
	}
}
