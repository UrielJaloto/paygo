package main

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/infrastructure/clock"
	"github.com/UrielJaloto/paygo/infrastructure/config"
	"github.com/UrielJaloto/paygo/infrastructure/identifier"
	"github.com/UrielJaloto/paygo/infrastructure/logging"
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

func newToolkit(telemetry config.Telemetry, stdout io.Writer, traces *tracing.Provider) toolkit {
	systemClock := clock.NewMoney()
	logger := logging.NewTracedJSONLogger(stdout, telemetry.LogLevel, traces)
	return toolkit{
		clock:    systemClock,
		ids:      identifier.NewMoney(systemClock, rand.Reader),
		requests: logging.NewRequestLogger(logger),
		tracer:   traces.Tracer(),
		registry: metrics.NewRegistry(),
	}
}

func openServices(ctx context.Context, settings config.APISettings, kit toolkit) (serviceSet, error) {
	pool, err := postgres.NewTracedPool(ctx, settings.Database.URL, settings.Database.MaxConns, tracing.NewQuerySpanStarter(kit.tracer))
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
			Authenticator: services.keys,
			Limiter:       ratelimit.NewMoney(kit.clock, settings.RateLimitPerSecond, settings.RateLimitBurst),
			Admin:         secret.NewAdminTokenChecker(settings.AdminToken),
		},
		Controllers: newControllers(services, kit),
	})
}

func newControllers(services serviceSet, kit toolkit) rest.Controllers {
	accounts, keys, webhooks := services.accounts, services.keys, services.webhooks
	transfers := metrics.NewTransferServiceMetrics(services.transfers, kit.registry)
	payments := metrics.NewPaymentServiceMetrics(services.payments, kit.registry)
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
		LedgerCheck:     rest.NewLedgerCheckController(services.ledger),
	}
}
