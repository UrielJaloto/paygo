package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/UrielJaloto/paygo/infrastructure/clock"
	"github.com/UrielJaloto/paygo/infrastructure/config"
	"github.com/UrielJaloto/paygo/infrastructure/identifier"
	"github.com/UrielJaloto/paygo/infrastructure/logging"
	"github.com/UrielJaloto/paygo/infrastructure/memory"
	"github.com/UrielJaloto/paygo/infrastructure/metrics"
	"github.com/UrielJaloto/paygo/infrastructure/poller"
	"github.com/UrielJaloto/paygo/infrastructure/postgres"
	"github.com/UrielJaloto/paygo/infrastructure/provider/asaas"
	"github.com/UrielJaloto/paygo/infrastructure/provider/fake"
	"github.com/UrielJaloto/paygo/infrastructure/randomness"
	"github.com/UrielJaloto/paygo/infrastructure/secret"
	"github.com/UrielJaloto/paygo/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/infrastructure/webhookhttp"
	"github.com/UrielJaloto/paygo/services/dispatch"
	"github.com/UrielJaloto/paygo/services/payment"
)

type toolkit struct {
	clock    clock.SystemClock
	ids      *identifier.UUIDv7Generator
	logger   *slog.Logger
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
	close    func()
}

func newToolkit(telemetry config.Telemetry, stdout io.Writer, traces *tracing.Provider) toolkit {
	systemClock := clock.New()
	logger := logging.NewTracedJSONLogger(stdout, telemetry.LogLevel, traces)
	return toolkit{
		clock:    systemClock,
		ids:      identifier.New(systemClock, rand.Reader),
		logger:   logger,
		requests: logging.NewRequestLogger(logger),
		tracer:   traces.Tracer(),
		registry: metrics.NewRegistry(),
	}
}

func openStorage(ctx context.Context, cfg config.WorkerConfig, kit toolkit) (storage, error) {
	if cfg.Payments.Storage == config.StorageMemory {
		return newMemoryStorage(cfg, kit), nil
	}
	pool, err := postgres.NewPool(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return storage{}, err
	}
	provider := newProvider(cfg.Payments, postgres.NewProviderCustomerCache(pool), kit)
	return storage{
		dispatch: dispatch.New(postgres.NewDispatchUnitOfWork(pool, kit.ids), postgres.NewDeliveryRepository(pool), newSender(cfg.Worker, kit), kit.clock, newJitter()),
		payments: payment.New(postgres.NewPaymentUnitOfWork(pool), provider, kit.clock, kit.ids),
		close:    pool.Close,
	}, nil
}

func newMemoryStorage(cfg config.WorkerConfig, kit toolkit) storage {
	store := memory.NewStore(kit.clock)
	provider := newProvider(cfg.Payments, memory.NewProviderCustomerCache(), kit)
	return storage{
		dispatch: dispatch.New(memory.NewDispatchUnitOfWork(store, kit.ids), memory.NewDeliveryRepository(store), newSender(cfg.Worker, kit), kit.clock, newJitter()),
		payments: payment.New(memory.NewPaymentUnitOfWork(store), provider, kit.clock, kit.ids),
		close:    func() {},
	}
}

func newJitter() *randomness.Jitter {
	return randomness.NewJitter(mathrand.Uint64())
}

func newSender(worker config.Worker, kit toolkit) *webhookhttp.HTTPWebhookSender {
	return webhookhttp.New(&http.Client{Timeout: worker.WebhookTimeout}, secret.WebhookSigner{}, kit.clock)
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
	dispatcher := logging.NewDispatchServiceLogger(metrics.NewDispatchServiceMetrics(tracing.NewDispatchServiceTracer(backend.dispatch, kit.tracer), kit.registry), kit.logger)
	payments := logging.NewPaymentServiceLogger(metrics.NewPaymentServiceMetrics(tracing.NewPaymentServiceTracer(backend.payments, kit.tracer), kit.registry), kit.logger)
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
