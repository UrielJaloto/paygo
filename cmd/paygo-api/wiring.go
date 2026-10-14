package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/infrastructure/clock"
	"github.com/UrielJaloto/paygo/infrastructure/config"
	"github.com/UrielJaloto/paygo/infrastructure/identifier"
	"github.com/UrielJaloto/paygo/infrastructure/logging"
	"github.com/UrielJaloto/paygo/infrastructure/memory"
	"github.com/UrielJaloto/paygo/infrastructure/metrics"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/infrastructure/provider/asaas"
	"github.com/UrielJaloto/paygo/infrastructure/provider/fake"
	"github.com/UrielJaloto/paygo/infrastructure/ratelimit"
	"github.com/UrielJaloto/paygo/infrastructure/secret"
	"github.com/UrielJaloto/paygo/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/presentation/rest"
	"github.com/UrielJaloto/paygo/services/account"
	"github.com/UrielJaloto/paygo/services/apikey"
	"github.com/UrielJaloto/paygo/services/ledgercheck"
	"github.com/UrielJaloto/paygo/services/payment"
	"github.com/UrielJaloto/paygo/services/transfer"
	"github.com/UrielJaloto/paygo/services/webhook"
)

type toolkit struct {
	clock    clock.SystemClock
	ids      *identifier.UUIDv7Generator
	logger   *slog.Logger
	requests logging.RequestLogger
	tracer   trace.Tracer
	registry *metrics.Registry
}

type serviceSet struct {
	accounts  *account.Service
	keys      *apikey.Service
	transfers *transfer.Service
	webhooks  *webhook.Service
	payments  *payment.Service
	ledger    *ledgercheck.Service
	readiness rest.ReadinessChecker
	close     func()
}

type alwaysReady struct{}

func (alwaysReady) IsReady(context.Context) bool {
	return true
}

func newToolkit(telemetry config.Telemetry, stdout io.Writer, traces *tracing.Provider) toolkit {
	systemClock := clock.NewMoney()
	logger := logging.NewTracedJSONLogger(stdout, telemetry.LogLevel, traces)
	return toolkit{
		clock:    systemClock,
		ids:      identifier.NewMoney(systemClock, rand.Reader),
		logger:   logger,
		requests: logging.NewRequestLogger(logger),
		tracer:   traces.Tracer(),
		registry: metrics.NewRegistry(),
	}
}

func openServices(ctx context.Context, settings config.APISettings, kit toolkit) (serviceSet, error) {
	if settings.Payments.Storage == config.StorageMemory {
		return newMemoryStorage(settings.Payments, kit), nil
	}
	pool, err := postgres.NewPool(ctx, settings.Database.URL, settings.Database.MaxConns)
	if err != nil {
		return serviceSet{}, err
	}
	ledger := postgres.NewLedgerRepository(pool)
	tokens := secret.NewTokenGenerator(rand.Reader)
	provider := newProvider(settings.Payments, postgres.NewProviderCustomerCache(pool), kit)
	return serviceSet{
		accounts:  account.NewMoney(postgres.NewAccountRepository(pool), ledger, kit.clock, kit.ids),
		keys:      apikey.NewMoney(postgres.NewAPIKeyRepository(pool), tokens, secret.TokenHasher{}, kit.clock, kit.ids),
		transfers: transfer.NewMoney(postgres.NewTransferUnitOfWork(pool), postgres.NewTransferRepository(pool), kit.clock, kit.ids),
		webhooks:  webhook.NewMoney(postgres.NewWebhookUnitOfWork(pool), tokens, kit.clock, kit.ids),
		payments:  payment.NewMoney(postgres.NewPaymentUnitOfWork(pool), provider, kit.clock, kit.ids),
		ledger:    ledgercheck.NewMoney(ledger),
		readiness: postgres.NewHealthChecker(pool),
		close:     pool.Close,
	}, nil
}

func newMemoryStorage(settings config.Payments, kit toolkit) serviceSet {
	store := memory.NewStore(kit.clock)
	ledger := memory.NewLedgerRepository(store)
	tokens := secret.NewTokenGenerator(rand.Reader)
	provider := newProvider(settings, memory.NewProviderCustomerCache(), kit)
	return serviceSet{
		accounts:  account.NewMoney(memory.NewAccountRepository(store), ledger, kit.clock, kit.ids),
		keys:      apikey.NewMoney(memory.NewAPIKeyRepository(store), tokens, secret.TokenHasher{}, kit.clock, kit.ids),
		transfers: transfer.NewMoney(memory.NewTransferUnitOfWork(store), memory.NewTransferRepository(store), kit.clock, kit.ids),
		webhooks:  webhook.NewMoney(memory.NewWebhookUnitOfWork(store), tokens, kit.clock, kit.ids),
		payments:  payment.NewMoney(memory.NewPaymentUnitOfWork(store), provider, kit.clock, kit.ids),
		ledger:    ledgercheck.NewMoney(ledger),
		readiness: alwaysReady{},
		close:     func() {},
	}
}

func newProvider(settings config.Payments, customers asaas.CustomerCache, kit toolkit) payment.PaymentProvider {
	var base payment.PaymentProvider = fake.NewMoney(settings.FakeWebhookToken, kit.ids)
	if settings.Provider == config.ProviderAsaas {
		client := &http.Client{Timeout: providerClientTimeout}
		base = asaas.NewMoney(settings.Asaas.BaseURL, settings.Asaas.APIKey, settings.Asaas.WebhookToken, customers, client)
	}
	return metrics.NewProviderMetrics(tracing.NewProviderTracer(base, kit.tracer), kit.registry)
}

func newRouter(settings config.HTTP, services serviceSet, kit toolkit) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	keys := logging.NewAPIKeyServiceLogger(tracing.NewAPIKeyServiceTracer(services.keys, kit.tracer), kit.logger)
	return rest.NewRouter(rest.Dependencies{
		Middleware: rest.Middleware{
			IDs:            kit.ids,
			RequestContext: kit.requests,
			Panics:         kit.requests,
			RequestTimeout: settings.RequestTimeout,
			Observability: rest.Observability{
				Logger:   kit.requests,
				Observer: kit.registry,
				Spans:    tracing.NewHTTPSpanStarter(kit.tracer),
				Clock:    kit.clock,
			},
		},
		Access: rest.Access{
			Authenticator: keys,
			Limiter:       ratelimit.NewMoney(kit.clock, settings.RateLimitPerSecond, settings.RateLimitBurst),
			Admin:         secret.NewAdminTokenChecker(settings.AdminToken),
		},
		Controllers: newControllers(services, keys, kit),
	})
}

func newControllers(services serviceSet, keys *logging.APIKeyServiceLogger, kit toolkit) rest.Controllers {
	accounts := logging.NewAccountServiceLogger(tracing.NewAccountServiceTracer(services.accounts, kit.tracer), kit.logger)
	transfers := logging.NewTransferServiceLogger(metrics.NewTransferServiceMetrics(tracing.NewTransferServiceTracer(services.transfers, kit.tracer), kit.registry), kit.logger)
	webhooks := logging.NewWebhookServiceLogger(tracing.NewWebhookServiceTracer(services.webhooks, kit.tracer), kit.logger)
	payments := logging.NewPaymentServiceLogger(metrics.NewPaymentServiceMetrics(tracing.NewPaymentServiceTracer(services.payments, kit.tracer), kit.registry), kit.logger)
	return rest.Controllers{
		Health:   rest.NewHealthController(services.readiness, kit.registry.Handler()),
		Account:  rest.NewAccountController(accounts, accounts, accounts, rest.AccountPresenter{}),
		Transfer: rest.NewTransferController(transfers, transfers, transfers, rest.TransferPresenter{}),
		Admin:    rest.NewAdminController(keys, keys, transfers, rest.AdminPresenter{}, rest.TransferPresenter{}),
		Webhook:  rest.NewWebhookController(webhooks, webhooks, webhooks, rest.WebhookPresenter{}),
		Payment: rest.NewPaymentController(rest.PaymentServices{
			ChargeCreator: payments,
			ChargeReader:  payments,
			PayoutCreator: payments,
			PayoutReader:  payments,
		}, rest.PaymentPresenter{}),
		ProviderWebhook: rest.NewProviderWebhookController(payments),
		LedgerCheck:     rest.NewLedgerCheckController(logging.NewLedgerCheckServiceLogger(services.ledger, kit.logger)),
	}
}
