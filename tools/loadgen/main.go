package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"time"
)

type options struct {
	scenario    string
	accounts    int
	transfers   int
	clients     int
	seed        uint64
	apiURL      string
	databaseURL string
	resultsDir  string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		stop()
		os.Exit(1)
	}
}

func parseOptions(args []string) (options, error) {
	var chosen options
	flags := flag.NewFlagSet("loadgen", flag.ContinueOnError)
	flags.StringVar(&chosen.scenario, "scenario", scenarioUniform, "uniform or contention")
	flags.IntVar(&chosen.accounts, "accounts", 1000, "accounts to create")
	flags.IntVar(&chosen.transfers, "transfers", 200000, "transfers to run")
	flags.IntVar(&chosen.clients, "clients", 64, "concurrent clients")
	flags.Uint64Var(&chosen.seed, "seed", 1, "random seed")
	flags.StringVar(&chosen.apiURL, "api-url", "http://localhost:18080", "paygo api base url")
	flags.StringVar(&chosen.databaseURL, "database-url", "postgres://paygo:paygo@localhost:55433/paygo?sslmode=disable", "postgres url for wal measurement, empty to skip")
	flags.StringVar(&chosen.resultsDir, "results", "results", "results directory")
	if err := flags.Parse(args); err != nil {
		return options{}, fmt.Errorf("parsing flags: %w", err)
	}
	return chosen, nil
}

func newClient(chosen options) *apiClient {
	return newAPIClient(clientConfig{
		baseURL:     chosen.apiURL,
		adminToken:  os.Getenv("PAYGO_ADMIN_TOKEN"),
		fundedMinor: fundingFor(chosen.transfers),
		connections: chosen.clients,
	})
}

func run(ctx context.Context, args []string, output io.Writer) error {
	chosen, err := parseOptions(args)
	if err != nil {
		return err
	}
	measured, err := measure(ctx, newClient(chosen), chosen)
	if err != nil {
		return err
	}
	path := resultPath(chosen.resultsDir, measured, time.Now())
	if err := writeReport(path, measured); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "%s: %.0f transfers/s, p99 %s, errors %.4f, correct %t\n", path, measured.TransfersPerSec, measured.Latency.P99, measured.ErrorRate, measured.Correct)
	return err
}

func measure(ctx context.Context, target wallet, chosen options) (report, error) {
	probe, postgres, err := openWALProbe(ctx, chosen.databaseURL)
	if err != nil {
		return report{}, err
	}
	defer probe.close(ctx)
	accounts, err := target.Setup(ctx, chosen.accounts)
	if err != nil {
		return report{}, fmt.Errorf("setting up accounts: %w", err)
	}
	if err := probe.mark(ctx); err != nil {
		return report{}, fmt.Errorf("marking wal: %w", err)
	}
	run := fmt.Sprintf("loadgen-%d", time.Now().UnixNano())
	load := workload{chosen.scenario, accounts, chosen.transfers, chosen.clients, chosen.seed, run}
	result, err := drive(ctx, target, load)
	if err != nil {
		return report{}, err
	}
	written, err := probe.bytesSinceMark(ctx)
	if err != nil {
		return report{}, err
	}
	balances, err := target.Balances(ctx)
	if err != nil {
		return report{}, fmt.Errorf("reading balances: %w", err)
	}
	measured := buildReport(chosen, result, written)
	measured.Versions.Postgres = postgres
	measured.Correct = correct(result.expected, balances)
	return measured, nil
}

func buildReport(chosen options, result outcome, walBytes int64) report {
	transfers := float64(chosen.transfers)
	return report{
		Scenario:         chosen.scenario,
		Accounts:         chosen.accounts,
		Transfers:        chosen.transfers,
		Clients:          chosen.clients,
		Seed:             chosen.seed,
		Hardware:         readHardware(),
		Versions:         versions{Go: runtime.Version()},
		TransfersPerSec:  transfers / result.elapsed.Seconds(),
		Latency:          summarize(result.samples),
		ErrorRate:        float64(result.failures) / transfers,
		WALBytesTransfer: float64(walBytes) / transfers,
	}
}
