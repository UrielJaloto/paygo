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
	minimumTokenLength = 32
	postgresScheme = "postgres://"
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
	RateLimitPerSecond    int
	RateLimitBurst  int
}

type Worker struct {
	PollInterval       time.Duration
	Concurrency        int
	WebhookTimeout     time.Duration
	WebhookMaximumAttempts int
	ShutdownTimeout    time.Duration
}

type Telemetry struct {
	LogLevel     slog.Level
	OTLPEndpoint string
}

type APISettings struct {
	Database
	HTTP
	Telemetry
}

type WorkerSettings struct {
	Database
	Worker
	Telemetry
}

func ReadAPI(lookup func(string) (string, bool)) (APISettings, error) {
	r := reader{lookup: lookup}
	settings := APISettings{Database: r.database(), HTTP: r.http(), Telemetry: r.telemetry()}
	if err := r.err(); err != nil {
		return APISettings{}, err
	}
	return settings, nil
}

func ReadWorker(lookup func(string) (string, bool)) (WorkerSettings, error) {
	r := reader{lookup: lookup}
	settings := WorkerSettings{Database: r.database(), Worker: r.worker(), Telemetry: r.telemetry()}
	if err := r.err(); err != nil {
		return WorkerSettings{}, err
	}
	return settings, nil
}

func ReadMigrate(lookup func(string) (string, bool)) (Database, error) {
	r := reader{lookup: lookup}
	settings := r.database()
	if err := r.err(); err != nil {
		return Database{}, err
	}
	return settings, nil
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

func (r *reader) recordFailure(name, reason string) {
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

func (r *reader) databaseURL() string {
	const name = "PAYGO_DATABASE_URL"
	url := r.text(name, "")
	if url == "" {
		r.recordFailure(name, "required")
		return ""
	}
	if !strings.HasPrefix(url, postgresScheme) {
		r.recordFailure(name, "must start with "+postgresScheme)
	}
	return url
}

func (r *reader) http() HTTP {
	settings := HTTP{
		Addr:            r.address("PAYGO_HTTP_ADDR", ":8080"),
		RequestTimeout:  r.duration(durationRule{name: "PAYGO_REQUEST_TIMEOUT", fallback: 10 * time.Second, min: time.Second, max: time.Minute}),
		ShutdownTimeout: r.shutdownTimeout(),
		AdminToken:      r.adminToken(),
		RateLimitPerSecond:    r.integer(integerRule{name: "PAYGO_RATE_LIMIT_RPS", fallback: 50, min: 1, max: 10000}),
		RateLimitBurst:  r.integer(integerRule{name: "PAYGO_RATE_LIMIT_BURST", fallback: 100, min: 1, max: 10000}),
	}
	if settings.RateLimitBurst > 0 && settings.RateLimitBurst < settings.RateLimitPerSecond {
		r.recordFailure("PAYGO_RATE_LIMIT_BURST", "must not be below PAYGO_RATE_LIMIT_RPS")
	}
	return settings
}

func (r *reader) adminToken() string {
	const name = "PAYGO_ADMIN_TOKEN"
	token := r.text(name, "")
	if len(token) < minimumTokenLength {
		r.recordFailure(name, fmt.Sprintf("must have at least %d characters", minimumTokenLength))
	}
	return token
}

func (r *reader) worker() Worker {
	return Worker{
		PollInterval:       r.duration(durationRule{name: "PAYGO_WORKER_POLL_INTERVAL", fallback: time.Second, min: 100 * time.Millisecond, max: time.Minute}),
		Concurrency:        r.integer(integerRule{name: "PAYGO_WORKER_CONCURRENCY", fallback: 8, min: 1, max: 64}),
		WebhookTimeout:     r.duration(durationRule{name: "PAYGO_WEBHOOK_TIMEOUT", fallback: 5 * time.Second, min: time.Second, max: 30 * time.Second}),
		WebhookMaximumAttempts: r.integer(integerRule{name: "PAYGO_WEBHOOK_MAX_ATTEMPTS", fallback: 8, min: 1, max: 20}),
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
	r.recordFailure(name, "must be one of debug, info, warn, error")
	return slog.LevelInfo
}

func (r *reader) address(name, fallback string) string {
	address := r.text(name, fallback)
	if address == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		r.recordFailure(name, "must be host:port")
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
		r.recordFailure(rule.name, fmt.Sprintf("must be a duration between %s and %s", rule.min, rule.max))
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
		r.recordFailure(rule.name, fmt.Sprintf("must be an integer between %d and %d", rule.min, rule.max))
		return 0
	}
	return value
}
