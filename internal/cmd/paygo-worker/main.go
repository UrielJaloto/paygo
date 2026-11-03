package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/trace"

	domainpayment "github.com/UrielJaloto/paygo/internal/domain/payment"
	"github.com/UrielJaloto/paygo/internal/domain/webhook"
	"github.com/UrielJaloto/paygo/internal/infrastructure/clock"
	"github.com/UrielJaloto/paygo/internal/infrastructure/config"
	"github.com/UrielJaloto/paygo/internal/infrastructure/httpserver"
	"github.com/UrielJaloto/paygo/internal/infrastructure/identifier"
	"github.com/UrielJaloto/paygo/internal/infrastructure/logging"
	"github.com/UrielJaloto/paygo/internal/infrastructure/metrics"
	"github.com/UrielJaloto/paygo/internal/infrastructure/poller"
	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/asaas"
	"github.com/UrielJaloto/paygo/internal/infrastructure/provider/fake"
	"github.com/UrielJaloto/paygo/internal/infrastructure/randomness"
	"github.com/UrielJaloto/paygo/internal/infrastructure/secret"
	"github.com/UrielJaloto/paygo/internal/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/internal/infrastructure/webhookhttp"
	"github.com/UrielJaloto/paygo/internal/services/dispatch"
	"github.com/UrielJaloto/paygo/internal/services/payment"
)

const (
	serviceName           = "paygo-worker"
	readHeaderTimeout     = 5 * time.Second
	tracingFlushTimeout   = 5 * time.Second
	providerClientTimeout = 30 * time.Second
	leaseMargin           = 30 * time.Second
	batchLimit            = 100
	fakeProviderName      = "fake"
	maxFakeMarkBytes      = 1 << 16
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.LookupEnv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if err := work(ctx, lookup, stdout); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", serviceName, err)
		return 1
	}
	return 0
}

func work(ctx context.Context, lookup func(string) (string, bool), stdout io.Writer) error {
	cfg, err := config.LoadWorker(lookup)
	if err != nil {
		return err
	}
	traces, err := tracing.NewProvider(ctx, cfg.Telemetry.OTLPEndpoint, serviceName)
	if err != nil {
		return err
	}
	worked := workTraced(ctx, cfg, newToolkit(cfg.Telemetry, stdout, traces))
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingFlushTimeout)
	defer cancel()
	return errors.Join(worked, traces.Shutdown(flush))
}

func workTraced(ctx context.Context, cfg config.WorkerConfig, kit toolkit) error {
	input, err := deliverInput(cfg.Worker)
	if err != nil {
		return err
	}
	backend, err := openStorage(ctx, cfg, kit)
	if err != nil {
		return err
	}
	defer backend.close()
	listener, err := httpserver.Listen(cfg.Worker.Addr)
	if err != nil {
		return err
	}
	server := httpserver.New(cfg.Worker.Addr, newMux(kit.registry.Handler(), backend.provider), readHeaderTimeout, cfg.Worker.ShutdownTimeout)
	serve := func(ctx context.Context) error { return server.Run(ctx, listener) }
	return poller.RunAll(ctx, append(newPollers(pollerPlan{worker: cfg.Worker, input: input, syncInterval: cfg.Payments.PaymentSyncInterval}, backend, kit), serve)...)
}

func deliverInput(worker config.Worker) (dispatch.DeliverInput, error) {
	policy, err := webhook.NewRetryPolicy(worker.WebhookMaxAttempts)
	if err != nil {
		return dispatch.DeliverInput{}, err
	}
	return dispatch.DeliverInput{Limit: worker.Concurrency, Lease: worker.WebhookTimeout + leaseMargin, Policy: policy}, nil
}

func markFakeState(provider payment.PaymentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFakeMarkBytes))
		_, err := provider.ParseWebhook(r.Context(), r.Header, body)
		switch {
		case errors.Is(err, domainpayment.ErrWebhookUnauthorized):
			w.WriteHeader(http.StatusUnauthorized)
		case err != nil:
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func newMux(metrics http.Handler, provider payment.PaymentProvider) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics)
	if provider.Name() == fakeProviderName {
		mux.Handle("POST /provider/v1/webhooks/fake", markFakeState(provider))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

type toolkit struct {
	clock    clock.SystemClock
	ids      *identifier.UUIDv7Generator
	requests logging.RequestLogger
	tracer   trace.Tracer
	registry *metrics.Registry
}

type pollerPlan struct {
	worker       config.Worker
	input        dispatch.DeliverInput
	syncInterval time.Duration
}

type storage struct {
	dispatch *dispatch.Service
	payments *payment.Service
	provider payment.PaymentProvider
	close    func()
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

func openStorage(ctx context.Context, cfg config.WorkerConfig, kit toolkit) (storage, error) {
	pool, err := postgres.NewTracedPool(ctx, cfg.Database.URL, cfg.Database.MaxConns, tracing.NewQuerySpanStarter(kit.tracer))
	if err != nil {
		return storage{}, err
	}
	provider := newProvider(cfg.Payments, postgres.NewProviderCustomerCache(pool), kit)
	return storage{
		dispatch: dispatch.New(postgres.NewDispatchUnitOfWork(pool, kit.ids), postgres.NewDeliveryRepository(pool), newSender(cfg.Worker, kit), kit.clock, newJitter()),
		payments: payment.New(postgres.NewPaymentUnitOfWork(pool), provider, kit.clock, kit.ids),
		provider: provider,
		close:    pool.Close,
	}, nil
}

func newJitter() *randomness.Jitter {
	return randomness.NewJitter(mathrand.Uint64())
}

func newSender(worker config.Worker, kit toolkit) *webhookhttp.HTTPWebhookSender {
	return webhookhttp.New(webhookhttp.NewClient(worker.WebhookTimeout), secret.WebhookSigner{}, kit.clock)
}

func newProvider(cfg config.Payments, customers asaas.CustomerCache, kit toolkit) payment.PaymentProvider {
	var base payment.PaymentProvider = fake.New(cfg.FakeWebhookToken, kit.ids)
	if cfg.Provider == config.ProviderAsaas {
		client := &http.Client{Timeout: providerClientTimeout}
		base = asaas.New(cfg.Asaas.BaseURL, cfg.Asaas.APIKey, cfg.Asaas.WebhookToken, customers, client)
	}
	return metrics.NewProviderMetrics(tracing.NewProviderTracer(base, kit.tracer), kit.registry)
}

func newPollers(plan pollerPlan, backend storage, kit toolkit) []func(context.Context) error {
	dispatcher := metrics.NewDispatchServiceMetrics(backend.dispatch, kit.registry)
	payments := metrics.NewPaymentServiceMetrics(backend.payments, kit.registry)
	drain := plan.worker.ShutdownTimeout
	outbound := poller.New(plan.worker.PollInterval, kit.requests)
	reconcile := poller.New(plan.syncInterval, kit.requests)
	return []func(context.Context) error{
		func(ctx context.Context) error {
			return outbound.Run(ctx, "fan-out", func(ctx context.Context) error {
				_, err := dispatcher.FanOut(ctx, batchLimit)
				return err
			}, drain)
		},
		func(ctx context.Context) error {
			return outbound.Run(ctx, "deliver", func(ctx context.Context) error {
				_, err := dispatcher.DeliverDue(ctx, plan.input)
				return err
			}, drain)
		},
		func(ctx context.Context) error {
			return reconcile.Run(ctx, "payment-sync", func(ctx context.Context) error {
				_, err := payments.Sync(ctx, batchLimit)
				return err
			}, drain)
		},
	}
}
