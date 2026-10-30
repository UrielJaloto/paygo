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
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, found := env[name]
		return value, found
	}
}

func validEnv(overrides map[string]string) func(string) (string, bool) {
	env := map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_ADMIN_TOKEN": adminToken}
	maps.Copy(env, overrides)
	return lookupFrom(env)
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

func TestLoadAPI_OnlyRequired_AppliesDefaults(t *testing.T) {
	cfg, err := config.LoadAPI(validEnv(nil))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	want := config.API{
		Database: config.Database{URL: databaseURL, MaxConns: 20},
		HTTP: config.HTTP{
			Addr: ":8080", RequestTimeout: 10 * time.Second, ShutdownTimeout: 15 * time.Second,
			AdminToken: adminToken, RateLimitRPS: 50, RateLimitBurst: 100,
		},
		Telemetry: config.Telemetry{LogLevel: slog.LevelInfo},
	}
	if cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadWorker_OnlyDatabase_AppliesDefaultsWithoutAdminToken(t *testing.T) {
	cfg, err := config.LoadWorker(lookupFrom(map[string]string{"PAYGO_DATABASE_URL": databaseURL}))
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	want := config.WorkerConfig{
		Database: config.Database{URL: databaseURL, MaxConns: 20},
		Worker: config.Worker{
			PollInterval: time.Second, Concurrency: 8, WebhookTimeout: 5 * time.Second,
			WebhookMaxAttempts: 8, ShutdownTimeout: 15 * time.Second,
		},
		Telemetry: config.Telemetry{LogLevel: slog.LevelInfo},
	}
	if cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadWorker_InvalidVariable_ReturnsError(t *testing.T) {
	_, err := config.LoadWorker(lookupFrom(map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_WORKER_CONCURRENCY": "65"}))
	assertInvalid(t, err, "PAYGO_WORKER_CONCURRENCY")
}

func TestLoadMigrate_DatabaseVariables_ReturnsDatabase(t *testing.T) {
	cfg, err := config.LoadMigrate(lookupFrom(map[string]string{"PAYGO_DATABASE_URL": databaseURL, "PAYGO_DB_MAX_CONNS": "5"}))
	if err != nil {
		t.Fatalf("LoadMigrate: %v", err)
	}
	if want := (config.Database{URL: databaseURL, MaxConns: 5}); cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadMigrate_MissingURL_ReturnsError(t *testing.T) {
	_, err := config.LoadMigrate(lookupFrom(nil))
	assertInvalid(t, err, "PAYGO_DATABASE_URL required")
}

func TestLoadAPI_InvalidRequired_ReturnsError(t *testing.T) {
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadAPI(lookupFrom(tt.env))
			assertInvalid(t, err, tt.want)
		})
	}
}

func TestLoadAPI_LogLevel_ParsesExactNames(t *testing.T) {
	tests := []struct {
		raw  string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			cfg, err := config.LoadAPI(validEnv(map[string]string{"PAYGO_LOG_LEVEL": tt.raw}))
			if err != nil {
				t.Fatalf("LoadAPI: %v", err)
			}
			if cfg.LogLevel != tt.want {
				t.Fatalf("level = %v, want %v", cfg.LogLevel, tt.want)
			}
		})
	}
}

func TestLoadAPI_UnknownLogLevel_ReturnsError(t *testing.T) {
	for _, raw := range []string{"DEBUG", "verbose"} {
		t.Run(raw, func(t *testing.T) {
			_, err := config.LoadAPI(validEnv(map[string]string{"PAYGO_LOG_LEVEL": raw}))
			assertInvalid(t, err, "PAYGO_LOG_LEVEL")
		})
	}
}

func TestLoadAPI_Boundaries_AcceptsOnlyValuesInRange(t *testing.T) {
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.LoadAPI(validEnv(map[string]string{tt.env: tt.value}))
			if tt.valid && err != nil {
				t.Fatalf("LoadAPI: %v", err)
			}
			if !tt.valid {
				assertInvalid(t, err, tt.env)
			}
		})
	}
}

func TestLoadAPI_RateLimitAtTopOfRange_Accepted(t *testing.T) {
	cfg, err := config.LoadAPI(validEnv(map[string]string{"PAYGO_RATE_LIMIT_RPS": "10000", "PAYGO_RATE_LIMIT_BURST": "10000"}))
	if err != nil {
		t.Fatalf("LoadAPI: %v", err)
	}
	if cfg.RateLimitRPS != 10000 || cfg.RateLimitBurst != 10000 {
		t.Fatalf("rate limit = %d/%d, want 10000/10000", cfg.RateLimitRPS, cfg.RateLimitBurst)
	}
}

func TestLoadAPI_BurstBelowRPS_ReturnsError(t *testing.T) {
	_, err := config.LoadAPI(validEnv(map[string]string{"PAYGO_RATE_LIMIT_RPS": "50", "PAYGO_RATE_LIMIT_BURST": "49"}))
	assertInvalid(t, err, "PAYGO_RATE_LIMIT_BURST must not be below PAYGO_RATE_LIMIT_RPS")
}

func TestLoadAPI_InvalidBurst_ReportsOnlyTheRange(t *testing.T) {
	_, err := config.LoadAPI(validEnv(map[string]string{"PAYGO_RATE_LIMIT_BURST": "0"}))
	assertInvalid(t, err, "PAYGO_RATE_LIMIT_BURST must be an integer")
	if strings.Contains(err.Error(), "must not be below") {
		t.Fatalf("err = %q, want a single burst failure", err)
	}
}

func TestLoadAPI_SeveralInvalid_JoinsEveryName(t *testing.T) {
	_, err := config.LoadAPI(lookupFrom(map[string]string{
		"PAYGO_DATABASE_URL":    "mysql://x",
		"PAYGO_ADMIN_TOKEN":     "short",
		"PAYGO_REQUEST_TIMEOUT": "abc",
	}))
	assertInvalid(t, err, "PAYGO_DATABASE_URL", "PAYGO_ADMIN_TOKEN", "PAYGO_REQUEST_TIMEOUT")
}

func TestLoadAPI_InvalidToken_DoesNotLeakValue(t *testing.T) {
	_, err := config.LoadAPI(validEnv(map[string]string{"PAYGO_ADMIN_TOKEN": "leaked-secret"}))
	if strings.Contains(err.Error(), "leaked-secret") {
		t.Fatalf("err = %q exposes the token", err)
	}
}
