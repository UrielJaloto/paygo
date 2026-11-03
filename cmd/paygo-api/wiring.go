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

type storage struct {
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
	systemClock := clock.New()
	logger := logging.NewTracedJSONLogger(stdout, telemetry.LogLevel, traces)
	return toolkit{
		clock:    systemClock,
		ids:      identifier.New(systemClock, rand.Reader),
		requests: logging.NewRequestLogger(logger),
		tracer:   traces.Tracer(),
		registry: metrics.NewRegistry(),
	}
}

func openStorage(ctx context.Context, cfg config.API, kit toolkit) (storage, error) {
	pool, err := postgres.NewTracedPool(ctx, cfg.Database.URL, cfg.Database.MaxConns, tracing.NewQuerySpanStarter(kit.tracer))
	if err != nil {
		return storage{}, err
	}
	ledger := postgres.NewLedgerRepository(pool)
	tokens := secret.NewTokenGenerator(rand.Reader)
	provider := newProvider(cfg.Payments, postgres.NewProviderCustomerCache(pool), kit)
	return storage{
		accounts:  account.New(postgres.NewAccountRepository(pool), ledger, kit.clock, kit.ids),
		keys:      apikey.New(postgres.NewAPIKeyRepository(pool), tokens, secret.TokenHasher{}, kit.clock, kit.ids),
		transfers: transfer.New(postgres.NewTransferUnitOfWork(pool), postgres.NewTransferRepository(pool), kit.clock, kit.ids),
		webhooks:  webhook.New(postgres.NewWebhookUnitOfWork(pool), tokens, kit.clock, kit.ids),
		payments:  payment.New(postgres.NewPaymentUnitOfWork(pool), provider, kit.clock, kit.ids),
		ledger:    ledgercheck.New(ledger),
		readiness: postgres.NewHealthChecker(pool),
		close:     pool.Close,
	}, nil
}

func newProvider(cfg config.Payments, customers asaas.CustomerCache, kit toolkit) payment.PaymentProvider {
	var base payment.PaymentProvider = fake.New(cfg.FakeWebhookToken, kit.ids)
	if cfg.Provider == config.ProviderAsaas {
		client := &http.Client{Timeout: providerClientTimeout}
		base = asaas.New(cfg.Asaas.BaseURL, cfg.Asaas.APIKey, cfg.Asaas.WebhookToken, customers, client)
	}
	return metrics.NewProviderMetrics(tracing.NewProviderTracer(base, kit.tracer), kit.registry)
}

func newRouter(cfg config.HTTP, backend storage, kit toolkit) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	return rest.NewRouter(rest.Dependencies{
		Middleware: rest.Middleware{
			IDs:            kit.ids,
			RequestContext: kit.requests,
			Panics:         kit.requests,
			RequestTimeout: cfg.RequestTimeout,
			Observability: rest.Observability{
				Logger:   kit.requests,
				Observer: kit.registry,
				Spans:    tracing.NewHTTPSpanStarter(kit.tracer),
				Clock:    kit.clock,
			},
		},
		Access: rest.Access{
			Authenticator: backend.keys,
			Limiter:       ratelimit.New(kit.clock, cfg.RateLimitRPS, cfg.RateLimitBurst),
			Admin:         secret.NewAdminTokenChecker(cfg.AdminToken),
		},
		Controllers: newControllers(backend, kit),
	})
}

func newControllers(backend storage, kit toolkit) rest.Controllers {
	accounts, keys, webhooks := backend.accounts, backend.keys, backend.webhooks
	transfers := metrics.NewTransferServiceMetrics(backend.transfers, kit.registry)
	payments := metrics.NewPaymentServiceMetrics(backend.payments, kit.registry)
	return rest.Controllers{
		Health:   rest.NewHealthController(backend.readiness, kit.registry.Handler()),
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
		LedgerCheck:     rest.NewLedgerCheckController(backend.ledger),
	}
}
