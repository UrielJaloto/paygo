package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	minTokenLength = 32
	postgresScheme = "postgres://"

	StorageMemory   = "memory"
	StoragePostgres = "postgres"
	ProviderFake    = "fake"
	ProviderAsaas   = "asaas"

	asaasProduction     = "production"
	asaasSandbox        = "sandbox"
	asaasProductionURL  = "https://api.asaas.com/v3"
	asaasSandboxURL     = "https://api-sandbox.asaas.com/v3"
	allowProductionName = "PAYGO_ASAAS_ALLOW_PRODUCTION"
)

var ErrInvalidConfig = errors.New("invalid configuration")

type Database struct {
	URL      string
	MaxConns int32
}

type HTTP struct {
	Addr            string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	AdminToken      string
	RateLimitRPS    int
	RateLimitBurst  int
}

type Worker struct {
	Addr               string
	PollInterval       time.Duration
	Concurrency        int
	WebhookTimeout     time.Duration
	WebhookMaxAttempts int
	ShutdownTimeout    time.Duration
}

type Telemetry struct {
	LogLevel     slog.Level
	OTLPEndpoint string
}

type Asaas struct {
	Env             string
	AllowProduction bool
	APIKey          string
	WebhookToken    string
	BaseURL         string
}

type Payments struct {
	Storage             string
	Provider            string
	FakeWebhookToken    string
	Asaas               Asaas
	PaymentSyncInterval time.Duration
}

type API struct {
	Database
	HTTP
	Telemetry
	Payments
}

type WorkerConfig struct {
	Database
	Worker
	Telemetry
	Payments
}

func LoadAPI(lookup func(string) (string, bool)) (API, error) {
	r := reader{lookup: lookup}
	payments := r.payments()
	cfg := API{Database: r.storageDatabase(payments.Storage), HTTP: r.http(), Telemetry: r.telemetry(), Payments: payments}
	if err := r.err(); err != nil {
		return API{}, err
	}
	return cfg, nil
}

func LoadWorker(lookup func(string) (string, bool)) (WorkerConfig, error) {
	r := reader{lookup: lookup}
	payments := r.payments()
	cfg := WorkerConfig{Database: r.storageDatabase(payments.Storage), Worker: r.worker(), Telemetry: r.telemetry(), Payments: payments}
	if err := r.err(); err != nil {
		return WorkerConfig{}, err
	}
	return cfg, nil
}

func LoadMigrate(lookup func(string) (string, bool)) (Database, error) {
	r := reader{lookup: lookup}
	cfg := r.database()
	if err := r.err(); err != nil {
		return Database{}, err
	}
	return cfg, nil
}

type durationRule struct {
	name     string
	fallback time.Duration
	min      time.Duration
	max      time.Duration
}

type integerRule struct {
	name     string
	fallback int
	min      int
	max      int
}

type reader struct {
	lookup   func(string) (string, bool)
	failures []error
}

func (r *reader) err() error {
	return errors.Join(r.failures...)
}

func (r *reader) fail(name, reason string) {
	r.failures = append(r.failures, fmt.Errorf("%w: %s %s", ErrInvalidConfig, name, reason))
}

func (r *reader) text(name, fallback string) string {
	value, found := r.lookup(name)
	if !found || value == "" {
		return fallback
	}
	return value
}

func (r *reader) database() Database {
	return Database{
		URL:      r.databaseURL(),
		MaxConns: int32(r.integer(integerRule{name: "PAYGO_DB_MAX_CONNS", fallback: 20, min: 1, max: 200})),
	}
}

func (r *reader) storageDatabase(storage string) Database {
	if storage == StorageMemory {
		return Database{}
	}
	return r.database()
}

func (r *reader) databaseURL() string {
	const name = "PAYGO_DATABASE_URL"
	url := r.text(name, "")
	if url == "" {
		r.fail(name, "required")
		return ""
	}
	if !strings.HasPrefix(url, postgresScheme) {
		r.fail(name, "must start with "+postgresScheme)
	}
	return url
}

func (r *reader) http() HTTP {
	cfg := HTTP{
		Addr:            r.address("PAYGO_HTTP_ADDR", ":8080"),
		RequestTimeout:  r.duration(durationRule{name: "PAYGO_REQUEST_TIMEOUT", fallback: 10 * time.Second, min: time.Second, max: time.Minute}),
		ShutdownTimeout: r.shutdownTimeout(),
		AdminToken:      r.adminToken(),
		RateLimitRPS:    r.integer(integerRule{name: "PAYGO_RATE_LIMIT_RPS", fallback: 50, min: 1, max: 10000}),
		RateLimitBurst:  r.integer(integerRule{name: "PAYGO_RATE_LIMIT_BURST", fallback: 100, min: 1, max: 10000}),
	}
	if cfg.RateLimitBurst > 0 && cfg.RateLimitBurst < cfg.RateLimitRPS {
		r.fail("PAYGO_RATE_LIMIT_BURST", "must not be below PAYGO_RATE_LIMIT_RPS")
	}
	return cfg
}

func (r *reader) adminToken() string {
	return r.token("PAYGO_ADMIN_TOKEN")
}

func (r *reader) token(name string) string {
	token := r.text(name, "")
	if len(token) < minTokenLength {
		r.fail(name, fmt.Sprintf("must have at least %d characters", minTokenLength))
	}
	return token
}

func (r *reader) payments() Payments {
	cfg := Payments{
		Storage:             r.choice("PAYGO_STORAGE", StoragePostgres, StorageMemory),
		Provider:            r.choice("PAYGO_PROVIDER", ProviderFake, ProviderAsaas),
		Asaas:               r.asaasSettings(),
		PaymentSyncInterval: r.duration(durationRule{name: "PAYGO_PAYMENT_SYNC_INTERVAL", fallback: 30 * time.Second, min: time.Second, max: 10 * time.Minute}),
	}
	switch cfg.Provider {
	case ProviderFake:
		cfg.FakeWebhookToken = r.token("PAYGO_FAKE_WEBHOOK_TOKEN")
	case ProviderAsaas:
		cfg.Asaas = r.requireAsaas(cfg.Asaas)
	}
	return cfg
}

func (r *reader) asaasSettings() Asaas {
	cfg := Asaas{
		Env:             r.choice("PAYGO_ASAAS_ENV", asaasSandbox, asaasProduction),
		AllowProduction: r.boolean(allowProductionName),
		APIKey:          r.text("PAYGO_ASAAS_API_KEY", ""),
		BaseURL:         asaasSandboxURL,
	}
	if cfg.Env == asaasProduction {
		cfg.BaseURL = asaasProductionURL
	}
	return cfg
}

func (r *reader) requireAsaas(cfg Asaas) Asaas {
	if cfg.APIKey == "" {
		r.fail("PAYGO_ASAAS_API_KEY", "required")
	}
	if cfg.Env == asaasProduction && !cfg.AllowProduction {
		r.fail(allowProductionName, "must be true when PAYGO_ASAAS_ENV is production")
	}
	cfg.WebhookToken = r.token("PAYGO_ASAAS_WEBHOOK_TOKEN")
	return cfg
}

func (r *reader) choice(name, fallback, other string) string {
	value := r.text(name, fallback)
	if value != fallback && value != other {
		r.fail(name, fmt.Sprintf("must be one of %s, %s", fallback, other))
	}
	return value
}

func (r *reader) boolean(name string) bool {
	value, err := strconv.ParseBool(r.text(name, "false"))
	if err != nil {
		r.fail(name, "must be true or false")
	}
	return value
}

func (r *reader) worker() Worker {
	return Worker{
		Addr:               r.address("PAYGO_HTTP_ADDR", ":8080"),
		PollInterval:       r.duration(durationRule{name: "PAYGO_WORKER_POLL_INTERVAL", fallback: time.Second, min: 100 * time.Millisecond, max: time.Minute}),
		Concurrency:        r.integer(integerRule{name: "PAYGO_WORKER_CONCURRENCY", fallback: 8, min: 1, max: 64}),
		WebhookTimeout:     r.duration(durationRule{name: "PAYGO_WEBHOOK_TIMEOUT", fallback: 5 * time.Second, min: time.Second, max: 30 * time.Second}),
		WebhookMaxAttempts: r.integer(integerRule{name: "PAYGO_WEBHOOK_MAX_ATTEMPTS", fallback: 8, min: 1, max: 20}),
		ShutdownTimeout:    r.shutdownTimeout(),
	}
}

func (r *reader) shutdownTimeout() time.Duration {
	return r.duration(durationRule{name: "PAYGO_SHUTDOWN_TIMEOUT", fallback: 15 * time.Second, min: time.Second, max: 2 * time.Minute})
}

func (r *reader) telemetry() Telemetry {
	return Telemetry{LogLevel: r.logLevel(), OTLPEndpoint: r.address("PAYGO_OTLP_ENDPOINT", "")}
}

func (r *reader) logLevel() slog.Level {
	const name = "PAYGO_LOG_LEVEL"
	switch r.text(name, "info") {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	r.fail(name, "must be one of debug, info, warn, error")
	return slog.LevelInfo
}

func (r *reader) address(name, fallback string) string {
	address := r.text(name, fallback)
	if address == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		r.fail(name, "must be host:port")
	}
	return address
}

func (r *reader) duration(rule durationRule) time.Duration {
	raw := r.text(rule.name, "")
	if raw == "" {
		return rule.fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < rule.min || value > rule.max {
		r.fail(rule.name, fmt.Sprintf("must be a duration between %s and %s", rule.min, rule.max))
		return 0
	}
	return value
}

func (r *reader) integer(rule integerRule) int {
	raw := r.text(rule.name, "")
	if raw == "" {
		return rule.fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < rule.min || value > rule.max {
		r.fail(rule.name, fmt.Sprintf("must be an integer between %d and %d", rule.min, rule.max))
		return 0
	}
	return value
}
