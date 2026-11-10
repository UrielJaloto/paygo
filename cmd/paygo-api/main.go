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
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/asaas"
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
	serviceName         = "paygo-api"
	readHeaderTimeout   = 5 * time.Second
	tracingFlushTimeout = 5 * time.Second
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
	settings, err := config.ReadAPI(lookup)
	if err != nil {
		return err
	}
	traces, err := tracing.NewProvider(ctx, settings.Telemetry.OTLPEndpoint, serviceName)
	if err != nil {
		return err
	}
	served := serveRequests(ctx, settings, newToolkit(settings.Telemetry, stdout, traces))
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingFlushTimeout)
	defer cancel()
	return errors.Join(served, traces.Shutdown(flush))
}

func serveRequests(ctx context.Context, settings config.APISettings, kit toolkit) error {
	services, err := openServices(ctx, settings, kit)
	if err != nil {
		return err
	}
	defer services.close()
	listener, err := httpserver.Listen(settings.HTTP.Addr)
	if err != nil {
		return err
	}
	server := httpserver.NewServer(settings.HTTP.Addr, newRouter(settings.HTTP, services, kit), readHeaderTimeout, settings.HTTP.ShutdownTimeout)
	return server.Run(ctx, listener)
}

type toolkit struct {
	now      func() time.Time
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
	logger := logging.NewTracedJSONLogger(stdout, telemetry.LogLevel, traces)
	return toolkit{
		now:      clock.Now,
		ids:      identifier.NewUUIDv7Generator(clock.Now, rand.Reader),
		requests: logging.NewRequestLogger(logger),
		tracer:   traces.Tracer(),
		registry: metrics.NewRegistry(),
	}
}

func openServices(ctx context.Context, settings config.APISettings, kit toolkit) (serviceSet, error) {
	pool, err := postgres.NewTracedPool(ctx, settings.Database.URL, settings.Database.MaximumConnections, tracing.NewQuerySpanStarter(kit.tracer))
	if err != nil {
		return serviceSet{}, err
	}
	ledger := postgres.NewLedgerRepository(pool)
	tokens := secret.NewTokenGenerator(rand.Reader)
	selected := newProvider(settings.Payments, postgres.NewProviderCustomerCache(pool), kit)
	return serviceSet{
		accounts:  account.NewService(postgres.NewAccountRepository(pool), ledger, kit.now, kit.ids),
		keys:      apikey.NewService(postgres.NewAPIKeyRepository(pool), tokens, secret.HashToken, kit.now, kit.ids),
		transfers: transfer.NewService(postgres.NewTransferUnitOfWork(pool), postgres.NewTransferRepository(pool), kit.now, kit.ids),
		webhooks:  webhook.NewService(postgres.NewWebhookUnitOfWork(pool), tokens, kit.now, kit.ids),
		payments:  payment.NewService(postgres.NewPaymentUnitOfWork(pool), selected, kit.now, kit.ids),
		ledger:    ledgercheck.NewService(ledger),
		readiness: postgres.NewHealthChecker(pool),
		close:     pool.Close,
	}, nil
}

func newProvider(settings config.Payments, customers asaas.CustomerCache, kit toolkit) payment.PaymentProvider {
	selected := provider.NewSelectedProvider(settings, customers, kit.ids)
	return metrics.NewProviderMetrics(tracing.NewProviderTracer(selected, kit.tracer), kit.registry)
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
				Now:      kit.now,
			},
		},
		Access: rest.Access{
			Authenticator: services.keys,
			Limiter:       ratelimit.NewTokenBucketLimiter(kit.now, settings.RateLimitPerSecond, settings.RateLimitBurst),
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
		Account:  rest.NewAccountController(accounts, accounts, accounts),
		Transfer: rest.NewTransferController(transfers, transfers, transfers),
		Admin:    rest.NewAdminController(keys, keys, transfers),
		Webhook:  rest.NewWebhookController(webhooks, webhooks, webhooks),
		Payment: rest.NewPaymentController(rest.PaymentServices{
			ChargeCreator: payments,
			ChargeReader:  payments,
			PayoutCreator: payments,
			PayoutReader:  payments,
		}),
		ProviderWebhook: rest.NewProviderWebhookController(payments),
		LedgerCheck:     rest.NewLedgerCheckController(services.ledger),
	}
}
