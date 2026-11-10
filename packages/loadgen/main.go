package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"time"
)

const (
	defaultAccounts    = 1000
	defaultTransfers   = 200000
	defaultClients     = 64
	defaultSeed        = 1
	defaultAPIURL      = "http://localhost:18080"
	defaultDatabaseURL = "postgres://paygo:example-postgres-password@localhost:55433/paygo?sslmode=disable"
	adminTokenVariable = "PAYGO_ADMIN_TOKEN"
)

var errMissingAdminToken = errors.New(adminTokenVariable + " is required")

type options struct {
	scenario    string
	accounts    int
	transfers   int
	clients     int
	seed        uint64
	apiURL      string
	databaseURL string
	out         string
	adminToken  string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, lookup func(string) (string, bool), output io.Writer) error {
	chosen, err := parseOptions(arguments, lookup)
	if err != nil {
		return err
	}
	measured, err := measureRun(ctx, newClient(chosen), chosen)
	if err != nil {
		return err
	}
	if chosen.out == "" {
		return writeReport(output, measured)
	}
	if err := saveReport(chosen.out, measured); err != nil {
		return err
	}
	_, err = io.WriteString(output, formatSummary(chosen.out, measured))
	return err
}

func parseOptions(arguments []string, lookup func(string) (string, bool)) (options, error) {
	var chosen options
	flags := flag.NewFlagSet("loadgen", flag.ContinueOnError)
	flags.StringVar(&chosen.scenario, "scenario", scenarioUniform, "uniform or contention")
	flags.IntVar(&chosen.accounts, "accounts", defaultAccounts, "accounts to create")
	flags.IntVar(&chosen.transfers, "transfers", defaultTransfers, "transfers to run")
	flags.IntVar(&chosen.clients, "clients", defaultClients, "concurrent clients")
	flags.Uint64Var(&chosen.seed, "seed", defaultSeed, "random seed")
	flags.StringVar(&chosen.apiURL, "api-url", defaultAPIURL, "paygo api base url")
	flags.StringVar(&chosen.databaseURL, "database-url", defaultDatabaseURL, "postgres url for wal measurement, empty to skip")
	flags.StringVar(&chosen.out, "out", "", "file for the json report, empty to print it")
	if err := flags.Parse(arguments); err != nil {
		return options{}, fmt.Errorf("parsing flags: %w", err)
	}
	token, found := lookup(adminTokenVariable)
	if !found || token == "" {
		return options{}, errMissingAdminToken
	}
	chosen.adminToken = token
	return chosen, nil
}

func newClient(chosen options) *apiClient {
	return newAPIClient(clientConfig{
		baseURL:     chosen.apiURL,
		adminToken:  chosen.adminToken,
		fundedMinor: calculateFunding(chosen.transfers),
		connections: chosen.clients,
	})
}

func measureRun(ctx context.Context, target wallet, chosen options) (report, error) {
	machine, err := readHardware()
	if err != nil {
		return report{}, err
	}
	probe, postgres, err := openWALProbe(ctx, chosen.databaseURL)
	if err != nil {
		return report{}, err
	}
	defer probe.close(ctx)
	result, written, err := runMeasuredLoad(ctx, target, probe, chosen)
	if err != nil {
		return report{}, err
	}
	balances, err := target.FetchBalances(ctx)
	if err != nil {
		return report{}, fmt.Errorf("reading balances: %w", err)
	}
	measured := chosen.buildReport(result, written)
	measured.Hardware = machine
	measured.Versions.Postgres = postgres
	measured.Correct = matchesBalances(result.expected, balances)
	return measured, nil
}

func runMeasuredLoad(ctx context.Context, target wallet, probe *walProbe, chosen options) (outcome, int64, error) {
	accounts, err := target.CreateAccounts(ctx, chosen.accounts)
	if err != nil {
		return outcome{}, 0, fmt.Errorf("setting up accounts: %w", err)
	}
	if err := probe.markPosition(ctx); err != nil {
		return outcome{}, 0, fmt.Errorf("marking wal: %w", err)
	}
	run := fmt.Sprintf("loadgen-%d", time.Now().UnixNano())
	result, err := driveLoad(ctx, target, workload{chosen.scenario, accounts, chosen.transfers, chosen.clients, chosen.seed, run})
	if err != nil {
		return outcome{}, 0, err
	}
	written, err := probe.measureWALSinceMark(ctx)
	return result, written, err
}

func (o options) buildReport(result outcome, walBytes int64) report {
	transfers := float64(o.transfers)
	return report{
		workloadSettings: workloadSettings{
			Scenario:  o.scenario,
			Accounts:  o.accounts,
			Transfers: o.transfers,
			Clients:   o.clients,
			Seed:      o.seed,
		},
		Versions: versions{Go: runtime.Version()},
		measurements: measurements{
			TransfersPerSecond:  transfers / result.elapsed.Seconds(),
			Latency:             summarizeLatencies(result.samples),
			ErrorRate:           float64(result.failures) / transfers,
			WALBytesPerTransfer: float64(walBytes) / transfers,
		},
	}
}
