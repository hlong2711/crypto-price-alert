package scheduler

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/notification"
	"crypto-price-alert/internal/repository"
	"crypto-price-alert/internal/service"

	"github.com/google/uuid"
)

// TargetExecutor runs independently configured alert jobs for each enabled target.
type TargetExecutor struct {
	periods   *PeriodEngine
	market    market.MarketDataProvider
	jobs      repository.JobRepository
	targets   repository.AlertTargetRepository
	configs   repository.AlertConfigRepository
	notifiers []notification.Notifier
	legacy    *Executor
	location  *time.Location
	mu        sync.Mutex
}

// NewTargetExecutor creates a coordinator-safe executor with a legacy fallback.
func NewTargetExecutor(
	periods *PeriodEngine,
	provider market.MarketDataProvider,
	jobs repository.JobRepository,
	targets repository.AlertTargetRepository,
	configs repository.AlertConfigRepository,
	notifiers []notification.Notifier,
	legacy *Executor,
	location *time.Location,
) (*TargetExecutor, error) {
	if periods == nil || provider == nil || jobs == nil || targets == nil || configs == nil || len(notifiers) == 0 || location == nil {
		return nil, fmt.Errorf("invalid target executor settings")
	}
	return &TargetExecutor{
		periods:   periods,
		market:    provider,
		jobs:      jobs,
		targets:   targets,
		configs:   configs,
		notifiers: notifiers,
		legacy:    legacy,
		location:  location,
	}, nil
}

// Execute loads targets and configurations at the period boundary; a duplicate tick is skipped while one is running.
func (e *TargetExecutor) Execute(ctx context.Context, now time.Time, interval domain.Interval) error {
	if !e.mu.TryLock() {
		return nil
	}
	defer e.mu.Unlock()
	targets, err := e.targets.ListEnabledTargets(ctx)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		if e.legacy == nil {
			return nil
		}
		return e.legacy.Execute(ctx, now, interval)
	} else {
		// TODO: REMOVE when all commands work: tmp run legacy exec for legacy group.
		go func() {
			e.legacy.Execute(ctx, now, interval)
		}()
	}

	period, due := e.periods.GetCurrentPeriod(now, interval)
	if !due {
		return nil
	}
	var firstErr error
	for _, target := range targets {
		config, err := e.configs.GetAlertConfig(ctx, target.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !config.Enabled || !containsInterval(config.Intervals, interval) {
			continue
		}
		if err := e.executeTarget(ctx, target, config, period); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *TargetExecutor) executeTarget(ctx context.Context, target domain.AlertTarget, config domain.AlertConfig, period domain.Period) error {
	type item struct {
		job    domain.Job
		result notification.PriceResult
	}
	items := make([]item, 0, len(config.Symbols))
	for _, symbol := range config.Symbols {
		job := domain.Job{
			ID:          uuid.NewString(),
			TargetID:    target.ID,
			Symbol:      symbol,
			Interval:    period.Interval,
			PeriodStart: period.Start,
			PeriodEnd:   period.End,
			Status:      domain.JobPending,
		}
		created, isNew, err := e.jobs.CreateIfNotExists(ctx, job)
		if err != nil {
			return err
		}
		if !isNew && created.Status == domain.JobSent {
			continue
		}
		candle, err := e.market.GetKline(ctx, symbol, period.Interval, period.Start, period.End)
		if err != nil {
			items = append(items, item{
				job:    created,
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: symbol}, Unavailable: true},
			})
			continue
		}

		change, err := service.CalculateChange(candle, period.Interval, period)
		if err != nil {
			items = append(items, item{
				job:    created,
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: symbol}, Unavailable: true},
			})
			continue
		}
		items = append(items, item{
			job:    created,
			result: notification.PriceResult{Change: change},
		})
	}
	if len(items) == 0 {
		return nil
	}
	results := make([]notification.PriceResult, 0, len(items))
	for _, item := range items {
		results = append(results, item.result)
	}
	message, err := notification.BuildMessage(period, results, e.location)
	if err != nil {
		return err
	}
	var sendErr error
	for _, notifier := range e.notifiers {
		if err := notification.SendToTarget(ctx, notifier, target, message); err != nil && sendErr == nil {
			sendErr = err
		}
	}
	for _, item := range items {
		if sendErr != nil {
			_ = e.jobs.MarkFailed(ctx, item.job.ID, sendErr.Error())
		} else {
			_ = e.jobs.MarkSent(ctx, item.job.ID, time.Now().UTC())
		}
	}
	return sendErr
}

func containsInterval(values []domain.Interval, wanted domain.Interval) bool {
	return slices.Contains(values, wanted)
}
