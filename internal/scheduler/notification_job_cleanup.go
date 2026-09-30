package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto-price-alert/internal/repository"

	"github.com/robfig/cron/v3"
)

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

func NewNotificationJobCleanupScheduler(
	location *time.Location,
	repository repository.NotificationJobCleanupRepository,
	retention time.Duration,
	logger *slog.Logger,
) (*NotificationJobCleanupScheduler, error) {
	if location == nil || repository == nil || retention <= 0 {
		return nil, fmt.Errorf("invalid notification job cleanup settings")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &NotificationJobCleanupScheduler{
		cron: cron.New(
			cron.WithLocation(location),
			cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)),
		),
		repository: repository,
		retention:  retention,
		logger:     logger,
	}, nil
}

func (s *NotificationJobCleanupScheduler) Start() error {
	_, err := s.cron.AddFunc(notificationJobCleanupCron, func() {
		now := time.Now()
		deleted, cleanupErr := s.cleanup(context.Background(), now)
		if cleanupErr != nil {
			s.logger.Error("notification job cleanup failed", "error", cleanupErr)
			return
		}
		cutoff := now.UTC().Add(-s.retention)
		s.logger.Info("notification job cleanup complete", "deleted_count", deleted, "cutoff", cutoff)
	})
	if err != nil {
		return fmt.Errorf("schedule notification job cleanup: %w", err)
	}
	s.cron.Start()
	return nil
}

func (s *NotificationJobCleanupScheduler) Stop() context.Context {
	return s.cron.Stop()
}

func (s *NotificationJobCleanupScheduler) cleanup(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-s.retention)
	var deletedTotal int64

	for {
		deleted, err := s.repository.DeleteNotificationJobsUpdatedBefore(ctx, cutoff, notificationJobCleanupBatchSize)
		if err != nil {
			return deletedTotal, err
		}
		deletedTotal += deleted
		if deleted < notificationJobCleanupBatchSize {
			return deletedTotal, nil
		}
	}
}
