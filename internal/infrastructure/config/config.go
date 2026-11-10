package config

import (
	"errors"
	"log/slog"
	"time"
)

const (
	ProviderFake  = "fake"
	ProviderAsaas = "asaas"
)

var ErrInvalidConfig = errors.New("invalid configuration")

type Database struct {
	URL                string
	MaximumConnections int32
}

type HTTP struct {
	Addr               string
	RequestTimeout     time.Duration
	ShutdownTimeout    time.Duration
	AdminToken         string
	RateLimitPerSecond int
	RateLimitBurst     int
}

type Worker struct {
	Addr                   string
	PollInterval           time.Duration
	Concurrency            int
	WebhookTimeout         time.Duration
	WebhookMaximumAttempts int
	ShutdownTimeout        time.Duration
}

type Telemetry struct {
	LogLevel     slog.Level
	OTLPEndpoint string
}

type Asaas struct {
	Environment     string
	AllowProduction bool
	APIKey          string
	WebhookToken    string
	BaseURL         string
}

type Payments struct {
	Provider            string
	FakeWebhookToken    string
	Asaas               Asaas
	PaymentSyncInterval time.Duration
}

type APISettings struct {
	Database
	HTTP
	Telemetry
	Payments
}

type WorkerSettings struct {
	Database
	Worker
	Telemetry
	Payments
}

type MigrateSettings struct {
	Database
}

func ReadAPI(lookup func(string) (string, bool)) (APISettings, error) {
	values := reader{lookup: lookup}
	settings := APISettings{Database: values.readDatabase(), HTTP: values.readHTTP(), Telemetry: values.readTelemetry(), Payments: values.readPayments()}
	if err := values.err(); err != nil {
		return APISettings{}, err
	}
	return settings, nil
}

func ReadWorker(lookup func(string) (string, bool)) (WorkerSettings, error) {
	values := reader{lookup: lookup}
	settings := WorkerSettings{Database: values.readDatabase(), Worker: values.readWorker(), Telemetry: values.readTelemetry(), Payments: values.readPayments()}
	if err := values.err(); err != nil {
		return WorkerSettings{}, err
	}
	return settings, nil
}

func ReadMigrate(lookup func(string) (string, bool)) (MigrateSettings, error) {
	values := reader{lookup: lookup}
	settings := MigrateSettings{Database: values.readDatabase()}
	if err := values.err(); err != nil {
		return MigrateSettings{}, err
	}
	return settings, nil
}
