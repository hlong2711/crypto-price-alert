# Notification-Job Retention Cleanup — Implementation Blueprint

## Goal and agreed behavior

Prevent unbounded growth of the `notification_jobs` PostgreSQL table.

- Add `database.notification_jobs_retention` as a YAML duration.
- `0s` or an omitted value disables cleanup, preserving compatibility with existing external configuration files.
- Shipped configuration files demonstrate `720h` (30 days).
- A job is stale when `updated_at` is strictly older than `now - retention`.
- Cleanup runs daily at `00:00` in `app.timezone`; it does not run at startup.
- Cleanup errors are logged and retried on the next scheduled run; they never stop alert delivery.

## 1. Add the retention configuration

Update `internal/config/config.go`.

Extend `DatabaseConfig`:

```go
type DatabaseConfig struct {
	URL                       string        `yaml:"url"`
	NotificationJobsRetention time.Duration `yaml:"notification_jobs_retention"`
}
```

`time.Duration` is already used by `RetryConfig`, so existing YAML duration parsing supports values such as `720h`, `168h`, and `0s`.

Add validation to `Config.Validate()` immediately after validation of `database.url`:

```go
if c.Database.NotificationJobsRetention < 0 {
	return errors.New("database.notification_jobs_retention must not be negative")
}
```

Do not apply a runtime default in `Load()`: a missing YAML value must remain zero and disable cleanup.

Update these files below their `database.url` setting:

- `configs/config.yaml`
- `configs/config.dev.yaml`
- `configs/config.example.yaml`

```yaml
database:
  url: "${DATABASE_URL}"
  # Delete notification_jobs not updated within this duration. Set 0s to disable.
  notification_jobs_retention: 720h
```

## 2. Add the cleanup index

Update `internal/database/model.go` so `NotificationJob.UpdatedAt` declares a named index:

```go
UpdatedAt time.Time `gorm:"not null;index:idx_notification_jobs_updated_at"`
```

No manual migration file is needed. The existing `database.Migrate()` already calls `AutoMigrate(&NotificationJob{})`, which creates the index if it is missing. The index supports the timestamp-filtered, oldest-first cleanup batches.

## 3. Define a narrow repository contract

Update `internal/repository/repository.go` with a cleanup-specific interface. Keep it separate from `JobRepository`, since alert delivery should not depend on deletion capability.

```go
type NotificationJobCleanupRepository interface {
	DeleteNotificationJobsUpdatedBefore(
		ctx context.Context,
		cutoff time.Time,
		limit int,
	) (int64, error)
}
```

`PostgresRepository` will satisfy both this interface and the existing job, target, and configuration repository interfaces.

## 4. Implement bounded deletion in PostgreSQL

Add `DeleteNotificationJobsUpdatedBefore` to `internal/repository/postgres.go`.

Implementation requirements:

1. Reject a `limit` less than one with `cleanup limit must be positive`.
2. Convert `cutoff` to UTC.
3. Select no more than `limit` candidate IDs where `updated_at < cutoff`, ordered ascending by `updated_at`.
4. Delete candidates using an outer `updated_at < cutoff` predicate as well.
5. Return the GORM result's `RowsAffected` value.
6. Wrap database failures as `delete stale notification jobs: %w`.

Suggested implementation:

```go
func (r *PostgresRepository) DeleteNotificationJobsUpdatedBefore(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	if limit < 1 {
		return 0, fmt.Errorf("cleanup limit must be positive")
	}

	cutoff = cutoff.UTC()
	candidates := r.db.WithContext(ctx).
		Model(&database.NotificationJob{}).
		Select("id").
		Where("updated_at < ?", cutoff).
		Order("updated_at ASC").
		Limit(limit)

	result := r.db.WithContext(ctx).
		Where("updated_at < ? AND id IN (?)", cutoff, candidates).
		Delete(&database.NotificationJob{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete stale notification jobs: %w", result.Error)
	}
	return result.RowsAffected, nil
}
```

The repeated outer timestamp condition protects a candidate that is updated after selection but before deletion. The scheduler must use a fixed private batch size of 1,000, with each batch as a separate statement, avoiding a single long-running delete transaction.

## 5. Add a dedicated cleanup scheduler

Create `internal/scheduler/notification_job_cleanup.go`; do not add cleanup work to the interval-based alert scheduler.

Define:

```go
const (
	notificationJobCleanupCron      = "0 0 * * *"
	notificationJobCleanupBatchSize = 1000
)

type NotificationJobCleanupScheduler struct {
	cron       *cron.Cron
	repository repository.NotificationJobCleanupRepository
	retention  time.Duration
	logger     *slog.Logger
}
```

Expose:

```go
func NewNotificationJobCleanupScheduler(
	location *time.Location,
	repository repository.NotificationJobCleanupRepository,
	retention time.Duration,
	logger *slog.Logger,
) (*NotificationJobCleanupScheduler, error)

func (s *NotificationJobCleanupScheduler) Start() error
func (s *NotificationJobCleanupScheduler) Stop() context.Context
```

Constructor rules:

- Reject nil `location` and nil `repository`.
- Reject `retention <= 0`; the caller must not create this scheduler when cleanup is disabled.
- Substitute `slog.Default()` for a nil logger.
- Create cron with both `cron.WithLocation(location)` and `cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger))` so cleanup cannot overlap itself.

Implement a package-private method for direct unit testing:

```go
func (s *NotificationJobCleanupScheduler) cleanup(
	ctx context.Context,
	now time.Time,
) (int64, error)
```

Its exact algorithm:

1. Calculate `cutoff := now.UTC().Add(-s.retention)` once for the entire run.
2. Call `DeleteNotificationJobsUpdatedBefore(ctx, cutoff, notificationJobCleanupBatchSize)`.
3. Add the returned count to a running total.
4. Stop when the deleted count is less than the batch size.
5. Return immediately on an error or canceled context.

In `Start`, register `notificationJobCleanupCron`. Its callback must call `cleanup(context.Background(), time.Now())` and either:

```go
s.logger.Info("notification job cleanup complete", "deleted_count", deleted, "cutoff", cutoff)
```

or:

```go
s.logger.Error("notification job cleanup failed", "error", err)
```

Do not treat a cleanup error as fatal. Do not invoke `cleanup` during construction or startup.

## 6. Wire scheduler lifecycle in the server

Update `cmd/server/main.go` after `jobRepo` and `location` have been created. Only create the maintenance scheduler when retention is positive:

```go
if cfg.Database.NotificationJobsRetention > 0 {
	cleanupScheduler, err := scheduler.NewNotificationJobCleanupScheduler(
		location,
		jobRepo,
		cfg.Database.NotificationJobsRetention,
		logger,
	)
	if err != nil {
		logger.Error("failed to initialize notification job cleanup scheduler", "error", err)
		os.Exit(1)
	}
	if err := cleanupScheduler.Start(); err != nil {
		logger.Error("failed to start notification job cleanup scheduler", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := cleanupScheduler.Stop().Err(); err != nil {
			logger.Error("notification job cleanup scheduler shutdown failed", "error", err)
		}
	}()
	logger.Info("notification job cleanup scheduler started", "retention", cfg.Database.NotificationJobsRetention)
}
```

Place the defer after database initialization, so Go's LIFO defer execution stops maintenance scheduling before `database.Close(db)` runs. Keep it independent from `jobScheduler`: stopping, failing, or disabling cleanup must not change alert scheduling.

## 7. Add tests

### Configuration tests

Update `internal/config/config_test.go`:

- A positive retention duration validates.
- `0s` validates and represents disabled cleanup.
- A negative duration fails validation.
- YAML loading parses `notification_jobs_retention: 720h` correctly.

### Cleanup scheduler tests

Create `internal/scheduler/notification_job_cleanup_test.go` with a fake `NotificationJobCleanupRepository` that records cutoffs, limits, return counts, and errors.

Cover:

1. One cleanup batch: verifies cutoff equals `now.UTC().Add(-retention)` and limit equals 1,000.
2. Multiple full batches: verifies cleanup continues until a short batch is returned and totals all rows.
3. Repository failure: verifies cleanup returns the error and does not make further calls.
4. Constructor validation: nil dependency, zero retention, and negative retention are rejected.
5. Daily schedule: verifies the cron spec is `0 0 * * *` and the supplied location is used.

### Repository tests

Add PostgreSQL-backed or SQL-mock coverage for `DeleteNotificationJobsUpdatedBefore`:

- Rejects a non-positive limit.
- Uses strict `< cutoff` rather than `<= cutoff`.
- Applies the supplied batch limit.
- Returns deleted row count.
- Keeps the outer cutoff predicate in the delete query.

## 8. Verification and acceptance criteria

Run:

```bash
go test ./...
go vet ./...
docker compose config
```

Manually verify against PostgreSQL:

1. Insert jobs whose `updated_at` values are older and newer than the selected retention cutoff.
2. Configure `database.notification_jobs_retention: 24h`.
3. Trigger the cleanup method in a controlled test or wait for local midnight.
4. Confirm that only rows with `updated_at < cutoff` were removed.
5. Confirm a row exactly on the cutoff remains until a later run.
6. Set the retention to `0s`, restart, and confirm the cleanup scheduler does not start.
7. Confirm normal job creation, `MarkSent`, `MarkFailed`, job uniqueness, and alert delivery remain unchanged.

## Out of scope

- Startup cleanup.
- Cleanup for any table besides `notification_jobs`.
- Configurable cleanup cadence or batch size.
- Changes to notification retry, status, uniqueness, or alert scheduling behavior.
