package scheduler

import (
	"context"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

type mockExecutor struct {
	executed map[domain.Interval]int
}

func (m *mockExecutor) Execute(ctx context.Context, now time.Time, interval domain.Interval) error {
	if m.executed == nil {
		m.executed = make(map[domain.Interval]int)
	}
	m.executed[interval]++
	return nil
}

func TestIntervalToCronSpec(t *testing.T) {
	tests := []struct {
		interval domain.Interval
		expected string
	}{
		{domain.Interval1M, "* * * * *"},
		{domain.Interval5M, "*/5 * * * *"},
		{domain.Interval15M, "*/15 * * * *"},
		{domain.Interval30M, "*/30 * * * *"},
		{domain.Interval1H, "0 * * * *"},
		{domain.Interval2H, "0 */2 * * *"},
		{domain.Interval4H, "0 */4 * * *"},
		{domain.Interval1D, "0 0 * * *"},
	}

	for _, tt := range tests {
		t.Run(string(tt.interval), func(t *testing.T) {
			spec, err := intervalToCronSpec(tt.interval)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if spec != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, spec)
			}
		})
	}
}

func TestNewSchedulerValidation(t *testing.T) {
	loc := time.UTC
	exec := &mockExecutor{}

	if _, err := NewScheduler(nil, exec, domain.Interval1M, []domain.Interval{domain.Interval1H}, "06:00", "23:00"); err == nil {
		t.Fatal("expected error for nil location")
	}

	if _, err := NewScheduler(loc, nil, domain.Interval1M, []domain.Interval{domain.Interval1H}, "06:00", "23:00"); err == nil {
		t.Fatal("expected error for nil executor")
	}

	if _, err := NewScheduler(loc, exec, domain.Interval1M, nil, "06:00", "23:00"); err == nil {
		t.Fatal("expected error for empty intervals")
	}

	if _, err := NewScheduler(loc, exec, domain.Interval("invalid"), []domain.Interval{domain.Interval1H}, "06:00", "23:00"); err == nil {
		t.Fatal("expected error for invalid tickInterval")
	}
}

func TestSchedulerTick(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	exec := &mockExecutor{}
	sched, err := NewScheduler(loc, exec, domain.Interval15M, []domain.Interval{domain.Interval15M, domain.Interval1H}, "06:00", "23:00")
	if err != nil {
		t.Fatal(err)
	}

	// 10:15 in Asia/Ho_Chi_Minh: 15m is due, 1h is not
	now := time.Date(2026, 9, 1, 10, 15, 0, 0, loc)
	sched.tick(context.Background(), now)

	if exec.executed[domain.Interval15M] != 1 {
		t.Fatalf("expected 15m to be executed once, got %d", exec.executed[domain.Interval15M])
	}
	if exec.executed[domain.Interval1H] != 0 {
		t.Fatalf("expected 1h to NOT be executed, got %d", exec.executed[domain.Interval1H])
	}
}
