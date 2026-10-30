package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	domainpayment "github.com/UrielJaloto/paygo/domain/payment"
	"github.com/UrielJaloto/paygo/domain/webhook"
	"github.com/UrielJaloto/paygo/infrastructure/config"
	"github.com/UrielJaloto/paygo/infrastructure/httpserver"
	"github.com/UrielJaloto/paygo/infrastructure/poller"
	"github.com/UrielJaloto/paygo/infrastructure/tracing"
	"github.com/UrielJaloto/paygo/services/dispatch"
	"github.com/UrielJaloto/paygo/services/payment"
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
