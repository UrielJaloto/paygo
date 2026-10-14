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
	settings, err := config.ReadAPI(lookup)
	if err != nil {
		return err
	}
	traces, err := tracing.NewProvider(ctx, settings.Telemetry.OTLPEndpoint, serviceName)
	if err != nil {
		return err
	}
	served := serveTraced(ctx, settings, newToolkit(settings.Telemetry, stdout, traces))
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingFlushTimeout)
	defer cancel()
	return errors.Join(served, traces.Shutdown(flush))
}

func serveTraced(ctx context.Context, settings config.APISettings, kit toolkit) error {
	services, err := openServices(ctx, settings, kit)
	if err != nil {
		return err
	}
	defer services.close()
	listener, err := httpserver.Listen(settings.HTTP.Addr)
	if err != nil {
		return err
	}
	server := httpserver.NewMoney(settings.HTTP.Addr, newRouter(settings.HTTP, services, kit), readHeaderTimeout, settings.HTTP.ShutdownTimeout)
	return server.Run(ctx, listener)
}
