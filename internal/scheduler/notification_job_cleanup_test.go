package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeNotificationJobCleanupRepository struct {
	limits  []int
	cutoffs []time.Time
	results []int64
	err     error
}

func (f *fakeNotificationJobCleanupRepository) DeleteNotificationJobsUpdatedBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return 0, f.err
	}
	result := int64(0)
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	return result, nil
}

func TestNotificationJobCleanupBatches(t *testing.T) {
	repo := &fakeNotificationJobCleanupRepository{results: []int64{1000, 1000, 2}}
	scheduler, err := NewNotificationJobCleanupScheduler(time.UTC, repo, 24*time.Hour, nil)
	if err != nil {
		t.Fatalf("create cleanup scheduler: %v", err)
	}

	now := time.Date(2026, 9, 22, 0, 0, 0, 123, time.FixedZone("local", 7*60*60))
	deleted, err := scheduler.cleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 2002 {
		t.Fatalf("expected 2002 deleted jobs, got %d", deleted)
	}
	expectedCutoff := now.UTC().Add(-24 * time.Hour)
	if len(repo.limits) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(repo.limits))
	}
	for _, limit := range repo.limits {
		if limit != notificationJobCleanupBatchSize {
			t.Fatalf("expected batch limit %d, got %d", notificationJobCleanupBatchSize, limit)
		}
	}
	for _, candidateCutoff := range repo.cutoffs {
		if !candidateCutoff.Equal(expectedCutoff) {
			t.Fatalf("expected every batch cutoff %v, got %v", expectedCutoff, candidateCutoff)
		}
	}
}

func TestNotificationJobCleanupStopsOnRepositoryError(t *testing.T) {
	repoErr := errors.New("database unavailable")
	repo := &fakeNotificationJobCleanupRepository{err: repoErr}
	scheduler, err := NewNotificationJobCleanupScheduler(time.UTC, repo, time.Hour, nil)
	if err != nil {
		t.Fatalf("create cleanup scheduler: %v", err)
	}

	deleted, cleanupErr := scheduler.cleanup(context.Background(), time.Now())
	if !errors.Is(cleanupErr, repoErr) {
		t.Fatalf("expected repository error, got %v", cleanupErr)
	}
	if deleted != 0 || len(repo.limits) != 1 {
		t.Fatalf("expected no deleted jobs and one repository call, got deleted=%d calls=%d", deleted, len(repo.limits))
	}
}

func TestNewNotificationJobCleanupSchedulerValidation(t *testing.T) {
	repo := &fakeNotificationJobCleanupRepository{}
	if _, err := NewNotificationJobCleanupScheduler(nil, repo, time.Hour, nil); err == nil {
		t.Fatal("expected nil location error")
	}
	if _, err := NewNotificationJobCleanupScheduler(time.UTC, nil, time.Hour, nil); err == nil {
		t.Fatal("expected nil repository error")
	}
	if _, err := NewNotificationJobCleanupScheduler(time.UTC, repo, 0, nil); err == nil {
		t.Fatal("expected zero retention error")
	}
	if _, err := NewNotificationJobCleanupScheduler(time.UTC, repo, -time.Hour, nil); err == nil {
		t.Fatal("expected negative retention error")
	}
}

func TestNotificationJobCleanupSchedule(t *testing.T) {
	if notificationJobCleanupCron != "0 0 * * *" {
		t.Fatalf("expected daily midnight schedule, got %q", notificationJobCleanupCron)
	}
}
