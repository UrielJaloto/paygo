# paygo

![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)
![Coverage](https://img.shields.io/badge/coverage-100%25-brightgreen)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

paygo is a digital wallet and double-entry payments ledger written in Go. It exposes a REST API for accounts, transfers and PIX payments, keeps every balance provable against an append-only journal, and delivers signed webhooks through a transactional outbox.

## Highlights

- **5,000 transfers per second with zero errors.** 200,000 concurrent transfers over HTTP, durable commits, and every balance matches the journal at the end.
- **No lost or duplicated money under concurrency.** Integration tests run 50 workers moving money at random, 200 opposite transfers racing for the same pair of accounts, and 100 requests sharing one idempotency key. The invariants hold every time.
- **100% test coverage, checked before every push.** Unit, integration and end-to-end suites run against real PostgreSQL and the real binaries.
- **Architecture checked by a test.** The build fails when a layer imports one it must not.
- **Contract-first API.** Every route is documented in OpenAPI, and the end-to-end suite calls every operation and checks the responses against it.

## Features

- Double-entry journal with materialized balances and an on-demand consistency check
- Accounts with statements, freeze and unfreeze
- Idempotent transfers and reversals, plus admin deposits and withdrawals
- PIX charges and payouts through a provider port (fake or Asaas)
- Signed webhooks with retries, a delivery log and replay
- API keys scoped by owner, rate limiting and an OpenAPI contract
- JSON logs, Prometheus metrics and OpenTelemetry traces

## Architecture

```mermaid
flowchart TD
    cmd["cmd: wiring only"] --> presentation["presentation/rest: Gin controllers"]
    cmd --> infrastructure["infrastructure: postgres, provider, webhooks, telemetry"]
    presentation --> services["services: application services, declare their own ports"]
    infrastructure --> services
    services --> domain["domain: entities and rules, standard library only"]
```

**Dependency rule.** Imports point inward. `domain` knows nothing about the outside, `services` declare the interfaces they consume, and `presentation` and `infrastructure` never import each other. [internal/test/architecture_test.go](internal/test/architecture_test.go) parses the source and fails the build when a rule is broken.

**Observability stays out of the services.** A Gin middleware logs and traces every request, pgx opens a span per query, the provider client is traced, and the money-moving services are wrapped with Prometheus counters in `main`. Services never import a logger or a tracer.

### How a transfer flows

```mermaid
sequenceDiagram
    participant Client
    participant API as paygo-api
    participant DB as PostgreSQL
    participant Worker as paygo-worker
    participant Receiver as Webhook receiver
    Client->>API: POST /v1/transfers + Idempotency-Key
    API->>DB: BEGIN
    API->>DB: look up the idempotency key
    API->>DB: lock both accounts, ascending id
    API->>DB: check funds
    API->>DB: insert entries and update balances, one statement
    API->>DB: insert the transfer and the outbox event
    API->>DB: COMMIT
    API-->>Client: 201 Created
    Worker->>DB: claim events FOR UPDATE SKIP LOCKED
    Worker->>Receiver: signed POST, retried with backoff
```

### Rules the ledger keeps

- **Double entry.** Every journal transaction balances: debits equal credits, in one currency.
- **Idempotency.** Money-moving requests carry an `Idempotency-Key`. Replaying it returns the original result, and reusing it with a different payload is a conflict.
- **Locking order.** Accounts in one operation are locked in ascending id order, which rules out deadlocks between opposite transfers.
- **Outbox.** Events are committed with the money movement, so neither can exist without the other.
- **Webhooks.** Delivery is at least once. Each request is signed with `Paygo-Signature: v1=<HMAC-SHA256>` over the timestamp and body, and receivers dedupe by `Paygo-Event-Id`.
- **PIX.** Charges and payouts post to the journal through clearing and hold accounts, driven by provider events.

## Performance

[packages/loadgen](packages/loadgen) opens 1,000 accounts, then sends 200,000 transfers from 64 concurrent clients over HTTP, each with its own idempotency key. Every request goes through the full path: API key check, rate limiter, idempotency lookup, row locks, funds check, journal, balances and outbox, in one transaction committed with `synchronous_commit=on`. At the end the generator compares every balance with the sum of the transfers that succeeded and reads WAL growth from PostgreSQL.

| Scenario | Transfers/s | p50 | p95 | p99 | Errors | WAL per transfer | Balances correct |
|---|---|---|---|---|---|---|---|
| uniform | 5,002 | 9.9 ms | 19.7 ms | 69.8 ms | 0 | 2.1 KB | yes |
| contention | 1,854 | 14.2 ms | 140.2 ms | 397.6 ms | 0 | 2.1 KB | yes |

In `uniform`, transfers pick accounts at random. In `contention`, 90% of them touch 10 hot accounts, so they queue on those rows' locks. Throughput drops, but no transfer fails, deadlocks or leaves a balance wrong.

Measured on an Intel Core i7-13700F with Docker Desktop on Windows (4 GB VM), PostgreSQL 18, and the load generator on the same machine. To reproduce:

```bash
docker compose -f env/load.compose.yaml up -d --build --wait
cd packages/loadgen
PAYGO_ADMIN_TOKEN=example-bench-admin-token-0123456789 go run . -scenario uniform
```

The JSON report (throughput, latency percentiles, WAL per transfer, hardware and versions) is printed to stdout; `-out <file>` saves it to a file instead.

## Testing

| Suite | What it proves |
|---|---|
| Unit | Domain rules, services with hand-written doubles, controllers through the Gin router, adapters against `httptest` servers |
| Fuzz | `money.ParseAmount` never panics and round-trips every amount it accepts |
| Architecture | Layers and domain contexts import only what the dependency rule allows |
| Integration | Repositories and units of work against real PostgreSQL, including the concurrency scenarios above |
| End-to-end | The three binaries running together: transfers, reversals, PIX, webhook retries and replay, owner isolation, rate limiting, graceful shutdown and the OpenAPI contract |

Unit tests:

```bash
go test -race ./...
```

All suites, using PostgreSQL at `PAYGO_TEST_DATABASE_URL`:

```bash
docker compose -f env/compose.yaml up -d postgres
```

```bash
go test -race -tags integration ./...
```

Every push is verified locally first: `gofmt`, `go vet`, builds for Linux, macOS and Windows, the unit, integration and end-to-end suites and a 100% coverage gate. The race detector needs cgo, so `-race` runs where a C toolchain is present.

## Quickstart

Requires Docker, `curl` and `jq`.

```bash
docker compose -f env/compose.yaml up -d --build
```

The credentials in `env/local.env.example` and `env/load.env.example` are placeholders for local stacks only; real deployments pass their own values through the environment.

If host port 8080 or 5432 is taken, set `PAYGO_API_PORT` or `PAYGO_POSTGRES_PORT` before starting, and adjust `API` below.

```bash
API=http://localhost:8080
ADMIN="Authorization: Bearer example-admin-token-0123456789abcdef"
FAKE="Paygo-Fake-Token: example-webhook-token-0123456789abcd"
OWNER=01890000-0000-7000-8000-000000000001
JSON="Content-Type: application/json"

TOKEN=$(curl -s -X POST $API/admin/v1/owners/$OWNER/api-keys -H "$ADMIN" | jq -r .token)
AUTH="Authorization: Bearer $TOKEN"

ALICE=$(curl -s -X POST $API/v1/accounts -H "$AUTH" -H "$JSON" -d '{"currency":"BRL"}' | jq -r .id)
BOB=$(curl -s -X POST $API/v1/accounts -H "$AUTH" -H "$JSON" -d '{"currency":"BRL"}' | jq -r .id)

curl -s -X POST $API/admin/v1/deposits -H "$ADMIN" -H "$JSON" -H "Idempotency-Key: deposit-1" \
  -d "{\"account_id\":\"$ALICE\",\"amount\":\"100.00\",\"currency\":\"BRL\"}"

curl -s -X POST $API/v1/transfers -H "$AUTH" -H "$JSON" -H "Idempotency-Key: transfer-1" \
  -d "{\"from_account_id\":\"$ALICE\",\"to_account_id\":\"$BOB\",\"amount\":\"30.00\",\"currency\":\"BRL\"}"

curl -s $API/v1/accounts/$ALICE/balance -H "$AUTH"

curl -s -X POST $API/v1/webhook-endpoints -H "$AUTH" -H "$JSON" -d '{"url":"https://example.com/paygo"}'

PROVIDER_ID=$(curl -s -X POST $API/v1/charges -H "$AUTH" -H "$JSON" -H "Idempotency-Key: charge-1" \
  -d "{\"account_id\":\"$BOB\",\"amount\":\"50.00\",\"currency\":\"BRL\",\"payer\":{\"name\":\"Ana Lima\",\"document\":\"52998224725\"}}" \
  | jq -r .pix_copy_paste | sed 's/^00020126fake//')

curl -s -X POST $API/provider/v1/webhooks/fake -H "$FAKE" -H "$JSON" \
  -d "{\"event_id\":\"paid-1\",\"type\":\"charge.paid\",\"provider_id\":\"$PROVIDER_ID\"}"

curl -s $API/v1/accounts/$BOB/balance -H "$AUTH"

curl -s $API/admin/v1/ledger/check -H "$ADMIN"
```

Alice ends with 70.00, Bob with 80.00, and the ledger check returns `"ok":true`.

## API

The full contract is in [docs/openapi.yaml](docs/openapi.yaml). A unit test checks that every route the router serves is documented, and the end-to-end suite calls every operation and checks the response fields against it.

| Area | Endpoints |
|---|---|
| Accounts | `POST /v1/accounts`, `GET /v1/accounts`, `GET /v1/accounts/{id}`, `GET /v1/accounts/{id}/balance`, `GET /v1/accounts/{id}/statement`, `POST /v1/accounts/{id}/freeze`, `POST /v1/accounts/{id}/unfreeze` |
| Transfers | `POST /v1/transfers`, `GET /v1/transfers`, `GET /v1/transfers/{id}`, `POST /v1/transfers/{id}/reversal` |
| Payments | `POST /v1/charges`, `GET /v1/charges/{id}`, `POST /v1/payouts`, `GET /v1/payouts/{id}`, `POST /provider/v1/webhooks/{provider}` |
| Webhooks | `POST /v1/webhook-endpoints`, `GET /v1/webhook-endpoints`, `DELETE /v1/webhook-endpoints/{id}`, `GET /v1/webhook-deliveries`, `POST /v1/webhook-deliveries/{id}/replay` |
| Admin | `POST /admin/v1/owners/{owner_id}/api-keys`, `DELETE /admin/v1/api-keys/{id}`, `POST /admin/v1/deposits`, `POST /admin/v1/withdrawals`, `GET /admin/v1/ledger/check` |
| Operations | `GET /healthz`, `GET /readyz`, `GET /metrics` |

### Provider modes

| Variable | Values | Effect |
|---|---|---|
| `PAYGO_PROVIDER` | `fake`, `asaas` | Runs PIX against the fake provider or Asaas |
| `PAYGO_ASAAS_ENV` | `sandbox`, `production` | Production also requires `PAYGO_ASAAS_ALLOW_PRODUCTION=true` |

## Observability

- Jaeger traces: <http://localhost:16686>
- Prometheus: <http://localhost:9090>
- Example metrics: `paygo_transfers_total`, `paygo_http_request_duration_seconds`, `paygo_webhook_deliveries_total`, `paygo_outbox_events_published_total`

## Design decisions

- **Polling outbox instead of a broker.** PostgreSQL is the only moving part, at the cost of some delivery latency and polling load. Workers claim events with `FOR UPDATE SKIP LOCKED`, so more of them can run side by side.
- **Materialized balances checked against the journal.** Balance reads do not scan entries, and `GET /admin/v1/ledger/check` proves they match.
- **Pessimistic locks in a fixed order.** Row locks keep balances exact without retry loops, and the ascending order removes deadlocks.
- **Gin confined to the edge.** The framework stays in `presentation/rest` and `cmd`, so services and domain never see it.
- **Hand-written doubles.** Tests use small stubs and fakes of the interfaces each unit declares, with no mock generator.
- **No ORM.** SQL is explicit, which keeps locks and query plans visible.

## Tech stack

Go 1.26, Gin, PostgreSQL 18 with pgx, OpenTelemetry, Prometheus and Docker Compose.

## Project layout

```text
cmd/               paygo-api, paygo-migrate and paygo-worker
internal/
  domain/          Entities and business rules
  services/        Application services
  presentation/    REST controllers and middleware
  infrastructure/  PostgreSQL with embedded migrations, PIX providers, webhook delivery, telemetry
  test/            Architecture rules and end-to-end suite
docs/              OpenAPI contract
env/               Compose files for the local and load test stacks
packages/          Load generator, a separate Go module
```

## License

[Apache License 2.0](LICENSE)
