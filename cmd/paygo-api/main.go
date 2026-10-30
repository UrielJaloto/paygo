package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/config"
	"github.com/UrielJaloto/paygo/infrastructure/httpserver"
	"github.com/UrielJaloto/paygo/infrastructure/tracing"
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
