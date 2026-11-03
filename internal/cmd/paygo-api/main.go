package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/internal/infrastructure/clock"
	"github.com/UrielJaloto/paygo/internal/infrastructure/config"
	"github.com/UrielJaloto/paygo/internal/infrastructure/httpserver"
	"github.com/UrielJaloto/paygo/internal/infrastructure/identifier"
	"github.com/UrielJaloto/paygo/internal/infrastructure/logging"
	"github.com/UrielJaloto/paygo/internal/infrastructure/metrics"
	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/asaas"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/fake"
	"github.com/UrielJaloto/paygo/internal/infrastructure/ratelimit"
	"github.com/UrielJaloto/paygo/internal/infrastructure/secret"
	"github.com/UrielJaloto/paygo/internal/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/internal/presentation/rest"
	"github.com/UrielJaloto/paygo/internal/services/account"
	"github.com/UrielJaloto/paygo/internal/services/apikey"
	"github.com/UrielJaloto/paygo/internal/services/ledgercheck"
	"github.com/UrielJaloto/paygo/internal/services/payment"
	"github.com/UrielJaloto/paygo/internal/services/transfer"
	"github.com/UrielJaloto/paygo/internal/services/webhook"
)

const (
	serviceName           = "paygo-api"
	readHeaderTimeout     = 5 * time.Second
	tracingFlushTimeout   = 5 * time.Second
	providerClientTimeout = 30 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.LookupEnv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if err := serve(ctx, lookup, stdout); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", serviceName, err)
		return 1
	}
	return 0
}

func serve(ctx context.Context, lookup func(string) (string, bool), stdout io.Writer) error {
	cfg, err := config.LoadAPI(lookup)
	if err != nil {
		return err
	}
	traces, err := tracing.NewProvider(ctx, cfg.Telemetry.OTLPEndpoint, serviceName)
	if err != nil {
		return err
	}
	served := serveTraced(ctx, cfg, newToolkit(cfg.Telemetry, stdout, traces))
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingFlushTimeout)
	defer cancel()
	return errors.Join(served, traces.Shutdown(flush))
}

func serveTraced(ctx context.Context, cfg config.API, kit toolkit) error {
	backend, err := openStorage(ctx, cfg, kit)
	if err != nil {
		return err
	}
	defer backend.close()
	listener, err := httpserver.Listen(cfg.HTTP.Addr)
	if err != nil {
		return err
	}
	server := httpserver.New(cfg.HTTP.Addr, newRouter(cfg.HTTP, backend, kit), readHeaderTimeout, cfg.HTTP.ShutdownTimeout)
	return server.Run(ctx, listener)
}

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
