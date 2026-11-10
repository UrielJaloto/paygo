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
	postgresScheme     = "postgres://"

	defaultHTTPAddr           = ":8080"
	defaultLogLevel           = "info"
	defaultMaximumConnections = 20
	maximumConnectionsLimit   = 200
	defaultRequestTimeout     = 10 * time.Second
	maximumRequestTimeout     = time.Minute
	defaultShutdownTimeout    = 15 * time.Second
	maximumShutdownTimeout    = 2 * time.Minute
	defaultRateLimitPerSecond = 50
	defaultRateLimitBurst     = 100
	maximumRateLimit          = 10000
	defaultPollInterval       = time.Second
	minimumPollInterval       = 100 * time.Millisecond
	maximumPollInterval       = time.Minute
	defaultConcurrency        = 8
	maximumConcurrency        = 64
	defaultWebhookTimeout     = 5 * time.Second
	maximumWebhookTimeout     = 30 * time.Second
	defaultWebhookAttempts    = 8
	maximumWebhookAttempts    = 20
	defaultPaymentSync        = 30 * time.Second
	maximumPaymentSync        = 10 * time.Minute
	minimumDuration           = time.Second
	minimumInteger            = 1
	asaasProduction           = "production"
	asaasSandbox              = "sandbox"
	asaasProductionURL        = "https://api.asaas.com/v3"
	asaasSandboxURL           = "https://api-sandbox.asaas.com/v3"
	allowProductionVariable   = "PAYGO_ASAAS_ALLOW_PRODUCTION"
	rateLimitBurstVariable    = "PAYGO_RATE_LIMIT_BURST"
	asaasAPIKeyVariable       = "PAYGO_ASAAS_API_KEY"
	databaseURLVariable       = "PAYGO_DATABASE_URL"
	logLevelVariable          = "PAYGO_LOG_LEVEL"
	shutdownTimeoutVariable   = "PAYGO_SHUTDOWN_TIMEOUT"
	httpAddrVariable          = "PAYGO_HTTP_ADDR"
)

type durationRule struct {
	name     string
	fallback time.Duration
	minimum  time.Duration
	maximum  time.Duration
}

type intRule struct {
	name     string
	fallback int
	minimum  int
	maximum  int
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

func (r *reader) readDatabase() Database {
	return Database{
		URL:                r.readDatabaseURL(),
		MaximumConnections: int32(r.readInt(intRule{name: "PAYGO_DB_MAX_CONNS", fallback: defaultMaximumConnections, minimum: minimumInteger, maximum: maximumConnectionsLimit})),
	}
}

func (r *reader) readDatabaseURL() string {
	url := r.readString(databaseURLVariable, "")
	if url == "" {
		r.recordFailure(databaseURLVariable, "required")
		return ""
	}
	if !strings.HasPrefix(url, postgresScheme) {
		r.recordFailure(databaseURLVariable, "must start with "+postgresScheme)
	}
	return url
}

func (r *reader) readHTTP() HTTP {
	settings := HTTP{
		Addr:               r.readAddress(httpAddrVariable, defaultHTTPAddr),
		RequestTimeout:     r.readDuration(durationRule{name: "PAYGO_REQUEST_TIMEOUT", fallback: defaultRequestTimeout, minimum: minimumDuration, maximum: maximumRequestTimeout}),
		ShutdownTimeout:    r.readShutdownTimeout(),
		AdminToken:         r.readToken("PAYGO_ADMIN_TOKEN"),
		RateLimitPerSecond: r.readInt(intRule{name: "PAYGO_RATE_LIMIT_RPS", fallback: defaultRateLimitPerSecond, minimum: minimumInteger, maximum: maximumRateLimit}),
		RateLimitBurst:     r.readInt(intRule{name: rateLimitBurstVariable, fallback: defaultRateLimitBurst, minimum: minimumInteger, maximum: maximumRateLimit}),
	}
	if settings.RateLimitBurst > 0 && settings.RateLimitBurst < settings.RateLimitPerSecond {
		r.recordFailure(rateLimitBurstVariable, "must not be below PAYGO_RATE_LIMIT_RPS")
	}
	return settings
}

func (r *reader) readWorker() Worker {
	return Worker{
		Addr:                   r.readAddress(httpAddrVariable, defaultHTTPAddr),
		PollInterval:           r.readDuration(durationRule{name: "PAYGO_WORKER_POLL_INTERVAL", fallback: defaultPollInterval, minimum: minimumPollInterval, maximum: maximumPollInterval}),
		Concurrency:            r.readInt(intRule{name: "PAYGO_WORKER_CONCURRENCY", fallback: defaultConcurrency, minimum: minimumInteger, maximum: maximumConcurrency}),
		WebhookTimeout:         r.readDuration(durationRule{name: "PAYGO_WEBHOOK_TIMEOUT", fallback: defaultWebhookTimeout, minimum: minimumDuration, maximum: maximumWebhookTimeout}),
		WebhookMaximumAttempts: r.readInt(intRule{name: "PAYGO_WEBHOOK_MAX_ATTEMPTS", fallback: defaultWebhookAttempts, minimum: minimumInteger, maximum: maximumWebhookAttempts}),
		ShutdownTimeout:        r.readShutdownTimeout(),
	}
}

func (r *reader) readShutdownTimeout() time.Duration {
	return r.readDuration(durationRule{name: shutdownTimeoutVariable, fallback: defaultShutdownTimeout, minimum: minimumDuration, maximum: maximumShutdownTimeout})
}

func (r *reader) readTelemetry() Telemetry {
	return Telemetry{LogLevel: r.readLogLevel(), OTLPEndpoint: r.readAddress("PAYGO_OTLP_ENDPOINT", "")}
}

func (r *reader) readLogLevel() slog.Level {
	level, found := parseLogLevel(r.readString(logLevelVariable, defaultLogLevel))
	if !found {
		r.recordFailure(logLevelVariable, "must be one of debug, info, warn, error")
		return slog.LevelInfo
	}
	return level
}

func parseLogLevel(name string) (slog.Level, bool) {
	switch name {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

func (r *reader) readPayments() Payments {
	settings := Payments{
		Provider:            r.readChoice("PAYGO_PROVIDER", ProviderFake, ProviderAsaas),
		Asaas:               r.readAsaas(),
		PaymentSyncInterval: r.readDuration(durationRule{name: "PAYGO_PAYMENT_SYNC_INTERVAL", fallback: defaultPaymentSync, minimum: minimumDuration, maximum: maximumPaymentSync}),
	}
	if settings.Provider == ProviderAsaas {
		settings.Asaas = r.readAsaasCredentials(settings.Asaas)
		return settings
	}
	settings.FakeWebhookToken = r.readToken("PAYGO_FAKE_WEBHOOK_TOKEN")
	return settings
}

func (r *reader) readAsaas() Asaas {
	settings := Asaas{
		Environment:     r.readChoice("PAYGO_ASAAS_ENV", asaasSandbox, asaasProduction),
		AllowProduction: r.readBool(allowProductionVariable),
		APIKey:          r.readString(asaasAPIKeyVariable, ""),
		BaseURL:         asaasSandboxURL,
	}
	if settings.Environment == asaasProduction {
		settings.BaseURL = asaasProductionURL
	}
	return settings
}

func (r *reader) readAsaasCredentials(settings Asaas) Asaas {
	if settings.APIKey == "" {
		r.recordFailure(asaasAPIKeyVariable, "required")
	}
	if settings.Environment == asaasProduction && !settings.AllowProduction {
		r.recordFailure(allowProductionVariable, "must be true when PAYGO_ASAAS_ENV is production")
	}
	settings.WebhookToken = r.readToken("PAYGO_ASAAS_WEBHOOK_TOKEN")
	return settings
}

func (r *reader) readString(name, fallback string) string {
	value, found := r.lookup(name)
	if !found || value == "" {
		return fallback
	}
	return value
}

func (r *reader) readToken(name string) string {
	token := r.readString(name, "")
	if len(token) < minimumTokenLength {
		r.recordFailure(name, fmt.Sprintf("must have at least %d characters", minimumTokenLength))
	}
	return token
}

func (r *reader) readChoice(name, fallback, other string) string {
	value := r.readString(name, fallback)
	if value != fallback && value != other {
		r.recordFailure(name, fmt.Sprintf("must be one of %s, %s", fallback, other))
	}
	return value
}

func (r *reader) readBool(name string) bool {
	value, err := strconv.ParseBool(r.readString(name, "false"))
	if err != nil {
		r.recordFailure(name, "must be true or false")
	}
	return value
}

func (r *reader) readAddress(name, fallback string) string {
	address := r.readString(name, fallback)
	if address == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		r.recordFailure(name, "must be host:port")
	}
	return address
}

func (r *reader) readDuration(rule durationRule) time.Duration {
	raw := r.readString(rule.name, "")
	if raw == "" {
		return rule.fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < rule.minimum || value > rule.maximum {
		r.recordFailure(rule.name, fmt.Sprintf("must be a duration between %s and %s", rule.minimum, rule.maximum))
		return 0
	}
	return value
}

func (r *reader) readInt(rule intRule) int {
	raw := r.readString(rule.name, "")
	if raw == "" {
		return rule.fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < rule.minimum || value > rule.maximum {
		r.recordFailure(rule.name, fmt.Sprintf("must be an integer between %d and %d", rule.minimum, rule.maximum))
		return 0
	}
	return value
}
