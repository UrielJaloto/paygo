package config_test

import (
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/UrielJaloto/paygo/infrastructure/config"
)

const (
	databaseURL = "postgres://paygo:paygo@localhost:5432/paygo"
	adminToken  = "0123456789abcdef0123456789abcdef"
	fakeToken   = "fedcba9876543210fedcba9876543210"
)

func buildLookup(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := env[name]
		return value, found
	}
}

func defaultPayments() config.Payments {
	return config.Payments{
		Storage: config.StoragePostgres, Provider: config.ProviderFake, FakeWebhookToken: fakeToken,
		Asaas:               config.Asaas{Env: "sandbox", BaseURL: "https://api-sandbox.asaas.com/v3"},
		PaymentSyncInterval: 30 * time.Second,
	}
}

func validEnvironment(overrides map[string]string) func(string) (string, bool) {
	env := map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_ADMIN_TOKEN": adminToken, "PAYGO_FAKE_WEBHOOK_TOKEN": fakeToken}
	maps.Copy(env, overrides)
	return buildLookup(env)
}

func assertInvalid(t *testing.T, err error, wants ...string) {
	t.Helper()
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to mention %q", err, want)
		}
	}
}

func TestReadAPI_OnlyRequired_AppliesDefaults(t *testing.T) {
	settings, err := config.ReadAPI(validEnvironment(nil))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	want := config.APISettings{
		Database: config.Database{URL: databaseURL, MaxConns: 20},
		HTTP: config.HTTP{
			Addr: ":8080", RequestTimeout: 10 * time.Second, ShutdownTimeout: 15 * time.Second,
			AdminToken: adminToken, RateLimitPerSecond: 50, RateLimitBurst: 100,
		},
		Telemetry: config.Telemetry{LogLevel: slog.LevelInfo},
		Payments:  defaultPayments(),
	}
	if settings != want {
		t.Fatalf("cfg = %+v, want %+v", settings, want)
	}
}

func TestReadWorker_OnlyDatabase_AppliesDefaultsWithoutAdminToken(t *testing.T) {
	settings, err := config.ReadWorker(buildLookup(map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_FAKE_WEBHOOK_TOKEN": fakeToken}))
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	want := config.WorkerSettings{
		Database: config.Database{URL: databaseURL, MaxConns: 20},
		Worker: config.Worker{
			Addr: ":8080", PollInterval: time.Second, Concurrency: 8, WebhookTimeout: 5 * time.Second,
			WebhookMaximumAttempts: 8, ShutdownTimeout: 15 * time.Second,
		},
		Telemetry: config.Telemetry{LogLevel: slog.LevelInfo},
		Payments:  defaultPayments(),
	}
	if settings != want {
		t.Fatalf("cfg = %+v, want %+v", settings, want)
	}
}

func TestReadWorker_InvalidVariable_ReturnsError(t *testing.T) {
	_, err := config.ReadWorker(buildLookup(map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_FAKE_WEBHOOK_TOKEN": fakeToken, "PAYGO_WORKER_CONCURRENCY": "65"}))
	assertInvalid(t, err, "PAYGO_WORKER_CONCURRENCY")
}

func TestLoadMigrate_DatabaseVariables_ReturnsDatabase(t *testing.T) {
	settings, err := config.ReadMigrate(buildLookup(map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_DB_MAX_CONNS": "5"}))
	if err != nil {
		t.Fatalf("LoadMigrate: %v", err)
	}
	if want := (config.Database{URL: databaseURL, MaxConns: 5}); settings != want {
		t.Fatalf("cfg = %+v, want %+v", settings, want)
	}
}

func TestReadMigrate_MissingURL_ReturnsError(t *testing.T) {
	_, err := config.ReadMigrate(buildLookup(nil))
	assertInvalid(t, err, "PAYGO_DATABASE_URL required")
}

func TestReadAPI_InvalidRequired_ReturnsError(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing database url", map[string]string{"PAYGO_ADMIN_TOKEN": adminToken}, "PAYGO_DATABASE_URL required"},
		{"url not postgres", map[string]string{"PAYGO_ADMIN_TOKEN": adminToken, "PAYGO_DATABASE_URL": "mysql://x"}, "PAYGO_DATABASE_URL must start with postgres://"},
		{"admin token 31 chars", map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_ADMIN_TOKEN": adminToken[:31]}, "PAYGO_ADMIN_TOKEN must have at least 32 characters"},
		{"admin token missing", map[string]string{"PAYGO_DATABASE_URL": databaseURL}, "PAYGO_ADMIN_TOKEN"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := config.ReadAPI(buildLookup(testCase.env))
			assertInvalid(t, err, testCase.want)
		})
	}
}

func TestReadAPI_LogLevel_ParsesExactNames(t *testing.T) {
	tests := []struct {
		raw  string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, testCase := range tests {
		t.Run(testCase.raw, func(t *testing.T) {
			settings, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_LOG_LEVEL": testCase.raw}))
			if err != nil {
				t.Fatalf("LoadAPI: %v", err)
			}
			if settings.LogLevel != testCase.want {
				t.Fatalf("level = %v, want %v", settings.LogLevel, testCase.want)
			}
		})
	}
}

func TestReadAPI_UnknownLogLevel_ReturnsError(t *testing.T) {
	for _, raw := range []string{"DEBUG", "verbose"} {
		t.Run(raw, func(t *testing.T) {
			_, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_LOG_LEVEL": raw}))
			assertInvalid(t, err, "PAYGO_LOG_LEVEL")
		})
	}
}

func TestReadAPI_Boundaries_AcceptsOnlyValuesInRange(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
		valid bool
	}{
		{"timeout minimum", "PAYGO_REQUEST_TIMEOUT", "1s", true},
		{"timeout maximum", "PAYGO_REQUEST_TIMEOUT", "60s", true},
		{"timeout below", "PAYGO_REQUEST_TIMEOUT", "999ms", false},
		{"timeout above", "PAYGO_REQUEST_TIMEOUT", "61s", false},
		{"timeout garbage", "PAYGO_REQUEST_TIMEOUT", "abc", false},
		{"rps minimum", "PAYGO_RATE_LIMIT_RPS", "1", true},
		{"rps equal to default burst", "PAYGO_RATE_LIMIT_RPS", "100", true},
		{"rps zero", "PAYGO_RATE_LIMIT_RPS", "0", false},
		{"rps above", "PAYGO_RATE_LIMIT_RPS", "10001", false},
		{"rps garbage", "PAYGO_RATE_LIMIT_RPS", "x", false},
		{"shutdown maximum", "PAYGO_SHUTDOWN_TIMEOUT", "120s", true},
		{"shutdown above", "PAYGO_SHUTDOWN_TIMEOUT", "121s", false},
		{"connections maximum", "PAYGO_DB_MAX_CONNS", "200", true},
		{"connections above", "PAYGO_DB_MAX_CONNS", "201", false},
		{"http address", "PAYGO_HTTP_ADDR", "127.0.0.1:9000", true},
		{"http address without port", "PAYGO_HTTP_ADDR", "localhost", false},
		{"otlp endpoint", "PAYGO_OTLP_ENDPOINT", "collector:4317", true},
		{"otlp endpoint without port", "PAYGO_OTLP_ENDPOINT", "collector", false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := config.ReadAPI(validEnvironment(map[string]string{testCase.env: testCase.value}))
			if testCase.valid && err != nil {
				t.Fatalf("LoadAPI: %v", err)
			}
			if !testCase.valid {
				assertInvalid(t, err, testCase.env)
			}
		})
	}
}

func TestReadAPI_RateLimitAtTopOfRange_Accepted(t *testing.T) {
	settings, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_RATE_LIMIT_RPS": "10000", "PAYGO_RATE_LIMIT_BURST": "10000"}))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	if settings.RateLimitPerSecond != 10000 || settings.RateLimitBurst != 10000 {
		t.Fatalf("rate limit = %d/%d, want 10000/10000", settings.RateLimitPerSecond, settings.RateLimitBurst)
	}
}

func TestReadAPI_BurstBelowRPS_ReturnsError(t *testing.T) {
	_, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_RATE_LIMIT_RPS": "50", "PAYGO_RATE_LIMIT_BURST": "49"}))
	assertInvalid(t, err, "PAYGO_RATE_LIMIT_BURST must not be below PAYGO_RATE_LIMIT_RPS")
}

func TestReadAPI_InvalidBurst_ReportsOnlyTheRange(t *testing.T) {
	_, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_RATE_LIMIT_BURST": "0"}))
	assertInvalid(t, err, "PAYGO_RATE_LIMIT_BURST must be an integer")
	if strings.Contains(err.Error(), "must not be below") {
		t.Fatalf("err = %q, want a single burst failure", err)
	}
}

func TestReadAPI_SeveralInvalid_JoinsEveryName(t *testing.T) {
	_, err := config.ReadAPI(buildLookup(map[string]string{
		"PAYGO_DATABASE_URL":    "mysql://x",
		"PAYGO_ADMIN_TOKEN":     "short",
		"PAYGO_REQUEST_TIMEOUT": "abc",
	}))
	assertInvalid(t, err, "PAYGO_DATABASE_URL", "PAYGO_ADMIN_TOKEN", "PAYGO_REQUEST_TIMEOUT")
}

func TestReadAPI_InvalidToken_DoesNotLeakValue(t *testing.T) {
	_, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_ADMIN_TOKEN": "leaked-secret"}))
	if strings.Contains(err.Error(), "leaked-secret") {
		t.Fatalf("err = %q exposes the token", err)
	}
}

func asaasEnvironment(overrides map[string]string) func(string) (string, bool) {
	env := map[string]string{"PAYGO_PROVIDER": "asaas", "PAYGO_ASAAS_API_KEY": "key", "PAYGO_ASAAS_WEBHOOK_TOKEN": fakeToken}
	maps.Copy(env, overrides)
	return validEnvironment(env)
}

func TestReadAPI_InvalidPayments_NamesVariable(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"production not allowed", map[string]string{"PAYGO_ASAAS_ENV": "production"}, "PAYGO_ASAAS_ALLOW_PRODUCTION"},
		{"allow not boolean", map[string]string{"PAYGO_ASAAS_ALLOW_PRODUCTION": "maybe"}, "PAYGO_ASAAS_ALLOW_PRODUCTION must be true or false"},
		{"missing key", map[string]string{"PAYGO_ASAAS_API_KEY": ""}, "PAYGO_ASAAS_API_KEY required"},
		{"short webhook token", map[string]string{"PAYGO_ASAAS_WEBHOOK_TOKEN": fakeToken[:31]}, "PAYGO_ASAAS_WEBHOOK_TOKEN must have at least 32 characters"},
		{"unknown env", map[string]string{"PAYGO_ASAAS_ENV": "staging"}, "PAYGO_ASAAS_ENV must be one of sandbox, production"},
		{"unknown storage", map[string]string{"PAYGO_STORAGE": "redis"}, "PAYGO_STORAGE must be one of postgres, memory"},
		{"unknown provider", map[string]string{"PAYGO_PROVIDER": "stripe"}, "PAYGO_PROVIDER must be one of fake, asaas"},
		{"sync below minimum", map[string]string{"PAYGO_PAYMENT_SYNC_INTERVAL": "999ms"}, "PAYGO_PAYMENT_SYNC_INTERVAL"},
		{"sync above maximum", map[string]string{"PAYGO_PAYMENT_SYNC_INTERVAL": "10m1s"}, "PAYGO_PAYMENT_SYNC_INTERVAL"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := config.ReadAPI(asaasEnvironment(testCase.env))
			assertInvalid(t, err, testCase.want)
		})
	}
}

func TestReadAPI_FakeProviderShortToken_ReturnsError(t *testing.T) {
	_, err := config.ReadAPI(validEnvironment(map[string]string{"PAYGO_FAKE_WEBHOOK_TOKEN": fakeToken[:31]}))
	assertInvalid(t, err, "PAYGO_FAKE_WEBHOOK_TOKEN must have at least 32 characters")
}

func TestReadAPI_AsaasProduction_UsesProductionURL(t *testing.T) {
	settings, err := config.ReadAPI(asaasEnvironment(map[string]string{"PAYGO_ASAAS_ENV": "production", "PAYGO_ASAAS_ALLOW_PRODUCTION": "true"}))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	want := config.Asaas{Env: "production", AllowProduction: true, APIKey: "key", WebhookToken: fakeToken, BaseURL: "https://api.asaas.com/v3"}
	if settings.Asaas != want {
		t.Fatalf("asaas = %+v, want %+v", settings.Asaas, want)
	}
}

func TestLoadAPI_MemoryStorage_DoesNotRequireDatabase(t *testing.T) {
	settings, err := config.ReadAPI(buildLookup(map[string]string{"PAYGO_STORAGE": "memory", "PAYGO_ADMIN_TOKEN": adminToken, "PAYGO_FAKE_WEBHOOK_TOKEN": fakeToken}))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	if settings.Storage != config.StorageMemory || settings.Database != (config.Database{}) {
		t.Fatalf("cfg = %+v, want memory storage without database", settings)
	}
}
