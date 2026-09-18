package scheduler

import (
	"context"
	"fmt"
	"time"

	"crypto-price-alert/internal/domain"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron         *cron.Cron
	executor     SchedulerExecutor
	tickInterval domain.Interval
	intervals    []domain.Interval
	periodEngine *PeriodEngine
}

// SchedulerExecutor is the scheduler operation invoked when a period boundary is reached.
type SchedulerExecutor interface {
	Execute(context.Context, time.Time, domain.Interval) error
}

func NewScheduler(location *time.Location, executor SchedulerExecutor, tickInterval domain.Interval, intervals []domain.Interval, activeFrom, activeUntil string) (*Scheduler, error) {
	if location == nil || executor == nil || len(intervals) == 0 {
		return nil, fmt.Errorf("invalid scheduler settings")
	}
	if tickInterval == "" {
		tickInterval = domain.Interval1M
	}
	if err := tickInterval.Validate(); err != nil {
		return nil, fmt.Errorf("invalid tick_interval: %w", err)
	}
	periodEngine, err := NewPeriodEngine(location, activeFrom, activeUntil)
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		cron:         cron.New(cron.WithLocation(location)),
		executor:     executor,
		tickInterval: tickInterval,
		intervals:    append([]domain.Interval(nil), intervals...),
		periodEngine: periodEngine,
	}, nil
}

func (s *Scheduler) Start() error {
	spec, err := intervalToCronSpec(s.tickInterval)
	if err != nil {
		return err
	}
	_, err = s.cron.AddFunc(spec, func() {
		s.tick(context.Background(), time.Now())
	})
	if err != nil {
		return err
	}
	s.cron.Start()
	return nil
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	for _, interval := range s.intervals {
		_, due := s.periodEngine.GetCurrentPeriod(now, interval)
		if due {
			_ = s.executor.Execute(ctx, now, interval)
		}
	}
}

func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}

func intervalToCronSpec(interval domain.Interval) (string, error) {
	dur, err := interval.Duration()
	if err != nil {
		return "", err
	}
	minutes := int(dur.Minutes())
	if minutes < 60 {
		if minutes <= 1 {
			return "* * * * *", nil
		}
		if 60%minutes == 0 {
			return fmt.Sprintf("*/%d * * * *", minutes), nil
		}
		return "* * * * *", nil // default to every minute
	}
	hours := int(dur.Hours())
	if hours < 24 {
		if hours == 1 {
			return "0 * * * *", nil
		}
		if 24%hours == 0 {
			return fmt.Sprintf("0 */%d * * *", hours), nil
		}
		return "0 * * * *", nil
	}
	return "0 0 * * *", nil //default to midnight
}
