# Crypto Price Alert MVP — Detailed Implementation Plan

## Summary

Implement a Go modular monolith with:

- PostgreSQL as the only MVP database.
- GORM for models, queries, and migrations.
- Echo for the HTTP/API layer.
- `robfig/cron` for scheduling.
- Binance as the market data provider.
- Telegram and Slack notification adapters.
- Idempotency enforced by a PostgreSQL unique constraint.
- Docker Compose with application and PostgreSQL services.

The latest database decision supersedes the earlier `sqlc` choice: use GORM only for MVP data access.

## Step 0 — Bootstrap and plan alignment

- Update remaining documentation references from `sqlc` to GORM.
- Initialize the Go module.
- Add Echo, GORM PostgreSQL driver, `robfig/cron`, YAML config, and test dependencies.
- Create the `cmd/`, `internal/`, `configs/`, and `migrations/` structure.
- Add `.gitignore`, `.env.example`, and example configuration.
- Define standard commands for formatting, vetting, testing, and building.

Acceptance criteria:

- `go mod tidy` succeeds.
- The project builds before database integration is added.
- No secret is committed to the repository.
- Package dependencies follow a one-way direction and domain code has no framework dependency.

## Step 1 — Configuration and application lifecycle

- Define configuration for timezone, HTTP address, PostgreSQL, Binance, symbols, intervals, active window, notification channels, retries, and concurrency.
- Load YAML configuration with environment-variable overrides for secrets and deployment values.
- Validate timezone, symbols, intervals, active hours, database settings, and enabled notification channels.
- Implement startup order: config → logger → database → services → scheduler → Echo server.
- Implement graceful shutdown for HTTP server, scheduler, and database connections.

Acceptance criteria:

- Valid configuration loads correctly.
- Missing or invalid configuration fails startup with a clear error.
- Secrets are read from environment variables.
- SIGINT/SIGTERM closes all resources cleanly.

## Step 2 — Domain models and interfaces

Create domain types for `Candle`, `PriceChange`, `Interval`, `Period`, `Job`, `JobStatus`, and `Message`.

Define interfaces:

```go
type MarketDataProvider interface {
    GetKline(ctx context.Context, symbol string, interval string, start, end time.Time) (Candle, error)
}

type Notifier interface {
    Send(ctx context.Context, message Message) error
}

type JobRepository interface {
    CreateIfNotExists(ctx context.Context, job Job) (Job, bool, error)
    MarkSent(ctx context.Context, id string, sentAt time.Time) error
    MarkFailed(ctx context.Context, id string, reason string) error
}
```

Acceptance criteria:

- Domain code does not import Echo, GORM, or provider-specific packages.
- Interval and job status values are validated.
- Interfaces can be exercised with fakes in unit tests.

## Step 3 — PostgreSQL schema and GORM migration

- Create the `notification_jobs` model with ID, symbol, interval, period start/end, status, error message, sent/created/updated timestamps.
- Add a unique constraint on `(symbol, interval, period_start)`.
- Connect using GORM with PostgreSQL.
- Configure connection pool limits and health checks.
- Run GORM `AutoMigrate` before the scheduler starts.
- Do not provide a SQLite fallback.

Acceptance criteria:

- The service connects to PostgreSQL.
- Startup migration creates the expected table and unique constraint.
- Data survives application restart.
- Recreating the same job key cannot create a second record.

## Step 4 — Repository and idempotency

- Implement `PostgresRepository` with GORM.
- Implement transaction-safe `CreateIfNotExists` behavior.
- Treat duplicate-key conflicts as an idempotent skip, not a fatal error.
- Support `pending`, `sent`, and `failed` states.
- Store failure reasons and `sent_at` where applicable.
- Mark a job sent only after the notification flow completes according to the delivery policy.

Acceptance criteria:

- Concurrent creation of the same job key results in one record.
- A sent job has `sent_at` populated.
- A failed job has status `failed` and an error reason.
- Repository tests cover duplicate and concurrent creation.

## Step 5 — Timezone and period engine

Implement:

```go
GetCurrentPeriod(now time.Time, interval Interval) (Period, bool)
```

Rules:

- Default timezone is `Asia/Ho_Chi_Minh`.
- Active range is `06:00` inclusive through `23:00` exclusive.
- 1H periods run from `06:00 → 07:00` through `22:00 → 23:00`.
- 4H periods are `06:00 → 10:00`, `10:00 → 14:00`, `14:00 → 18:00`, and `18:00 → 22:00`.
- `23:00 → 06:00` is inactive.
- `22:00 → 23:00` is not a valid 4H period.

Acceptance criteria:

- Tests cover `05:59`, `06:00`, `22:59`, and `23:00`.
- Tests cover every 1H and 4H period.
- UTC-to-`Asia/Ho_Chi_Minh` conversion is correct.
- No period overlaps or crosses the sleep window.

## Step 6 — Calculator

Implement:

```go
CalculateChange(open, close float64) (float64, error)
```

Use:

```text
(close - open) / open * 100
```

Reject non-positive or invalid opening prices. Keep full precision in the domain and round only during message formatting.

Acceptance criteria:

- `100 → 110` returns `10`.
- `100 → 90` returns `-10`.
- Equal prices return `0`.
- Invalid opening prices return errors.
- Calculator unit tests pass.

## Step 7 — Binance provider

- Implement a Binance public kline HTTP client.
- Request candles by symbol, exchange interval, and period boundaries.
- Parse and validate open, close, open time, and close time.
- Reject empty, malformed, or out-of-period responses.
- Configure HTTP timeout.
- Retry transient failures at most three times with approximately 500ms and 1s backoff.
- Retry timeout, connection reset, HTTP 5xx, and retryable rate-limit responses only.
- Limit concurrent symbol requests to five.

Acceptance criteria:

- Valid Binance responses map correctly to `Candle`.
- Timeout and provider errors do not crash the process.
- Retry count never exceeds configuration.
- Non-retryable 4xx errors return immediately.
- Fake-server tests cover success, malformed response, timeout, 5xx, and rate limiting.

## Step 8 — Message builder

- Build one aggregated message per interval and period.
- Include interval and configured local period time.
- Include symbol, close price, change percentage, and up/down indicator.
- Include `unavailable` for symbols whose market request failed.
- Format prices and percentages consistently.
- Escape content appropriately for Telegram and Slack.

Acceptance criteria:

- A batch produces one message containing all symbols.
- Successful and failed symbols can appear together.
- Positive, negative, and zero changes render correctly.
- Telegram and Slack contain equivalent information.
- Messages contain no credentials or unsafe raw secrets.

## Step 9 — Telegram and Slack notifiers

- Implement Telegram Bot API notifier.
- Implement Slack webhook notifier.
- Validate token/chat ID/webhook URL only when the channel is enabled.
- Configure HTTP timeout and transient-error retries.
- Never log tokens or webhook URLs.
- Attempt enabled channels independently so one channel failure does not prevent the other.
- After finite retries, return the final error and let the job be marked `failed`.

Acceptance criteria:

- Each notifier succeeds against a fake HTTP server.
- Disabled channels make no outbound request.
- One channel failure does not crash the batch.
- Retry count and final failure status are correct.

## Step 10 — Scheduler and job execution

Use `robfig/cron` with the configured timezone.

Triggers:

- 1H at the beginning of each active hour.
- 4H at `10:00`, `14:00`, `18:00`, and `22:00`.
- No trigger at `23:00` or during the sleep window.

Execution flow:

1. Resolve the completed period.
2. Create the job idempotently.
3. Skip if the job is already sent.
4. Fetch data for all symbols.
5. Calculate changes.
6. Build the aggregated message.
7. Send through enabled notifiers.
8. Mark sent on success or failed after retries are exhausted.

Do not implement startup catch-up for missed periods in the MVP.

Acceptance criteria:

- A 1H trigger at `10:00` processes `09:00 → 10:00`.
- A 4H trigger at `10:00` processes `06:00 → 10:00`.
- No notification is sent during `23:00 → 06:00`.
- Repeated scheduler invocation cannot duplicate a sent job.
- One symbol failure does not discard other symbols.
- Scheduler tests use a controllable clock or period resolver.

## Step 11 — Echo API and health check

- Initialize Echo as the HTTP server.
- Add `GET /health` returning:

```json
{"status":"ok"}
```

- Add recovery and request logging middleware.
- Keep handlers thin; business logic remains in application services.
- Do not expose credentials or internal failure details through health responses.

Acceptance criteria:

- `GET /health` returns HTTP 200 with the expected JSON.
- Echo starts and stops gracefully.
- Handler panics are recovered and logged.
- Health check works inside Docker.

## Step 12 — Docker deployment

- Create a multi-stage Dockerfile.
- Create Docker Compose services for `crypto-alert` and `postgres`.
- Add a persistent PostgreSQL volume.
- Pass runtime configuration through environment variables/config mounts.
- Add database readiness handling before application startup.
- Add a container healthcheck calling `/health`.
- Run as a non-root user where practical.
- Document setup, secrets, startup, logs, restart, and health verification.

Acceptance criteria:

- `docker compose up` starts both services successfully.
- The scheduler starts only after PostgreSQL is ready.
- `/health` returns `status=ok`.
- Restarting the application preserves job records.
- No secret is hard-coded in Dockerfile or Compose configuration.

## Step 13 — Full verification

Add and run:

- Unit tests for calculation, configuration, time windows, message formatting, retry classification, and scheduler decisions.
- Integration tests for GORM migration, repository idempotency, and PostgreSQL persistence.
- End-to-end tests with fake Binance, Telegram, and Slack servers.

Verification commands:

```text
go test ./...
go vet ./...
go build ./...
docker compose config
docker compose up
```

Acceptance criteria:

- All tests pass.
- `go vet ./...` reports no issues.
- The binary builds successfully.
- E2E coverage includes 1H, 4H, duplicate invocation, provider timeout, partial symbol failure, and notifier failure.
- Every MVP Definition of Done item is satisfied.

## Implementation dependency order

```text
Bootstrap
  ↓
Config + lifecycle
  ↓
Domain interfaces
  ├── PostgreSQL/GORM repository
  ├── Time/window engine
  ├── Calculator
  ├── Binance provider
  └── Notification adapters
          ↓
Message builder + job execution
          ↓
Scheduler
          ↓
Echo health API
          ↓
Docker + E2E verification
```

## Fixed assumptions

- GORM completely replaces `sqlc` for MVP data access.
- PostgreSQL is the only database; no SQLite fallback.
- GORM `AutoMigrate` is used for the initial MVP migration.
- `robfig/cron` is the scheduler.
- Notification retries are finite; exhausted retries mark the job `failed`.
- Missed periods are not replayed after restart.
- MVP exposes only `GET /health`; no authentication, web UI, or admin API.
- A job is keyed by symbol, interval, and period start; notifications are aggregated at execution time.
