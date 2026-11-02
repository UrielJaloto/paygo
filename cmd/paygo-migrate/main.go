package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/UrielJaloto/paygo/internal/infrastructure/config"
	"github.com/UrielJaloto/paygo/internal/infrastructure/logging"
	"github.com/UrielJaloto/paygo/internal/infrastructure/postgres"
)

func main() {
	os.Exit(run(context.Background(), os.LookupEnv, os.Stdout, os.Stderr))
}

func run(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	if err := migrate(ctx, lookup, stdout); err != nil {
		fmt.Fprintf(stderr, "paygo-migrate: %v\n", err)
		return 1
	}
	return 0
}

func migrate(ctx context.Context, lookup func(string) (string, bool), stdout io.Writer) error {
	settings, err := config.ReadMigrate(lookup)
	if err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, settings.URL, settings.MaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := postgres.NewMigrator(pool).Migrate(ctx)
	if err != nil {
		return err
	}
	logging.NewJSONLogger(stdout, slog.LevelInfo).InfoContext(ctx, "migrations applied", slog.Any("files", applied))
	return nil
}
