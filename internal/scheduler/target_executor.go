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
	}

	period, due := e.periods.GetCurrentPeriod(now, interval)
	if !due {
		return nil
	}
	var firstErr error
	eligible := make([]eligibleTarget, 0, len(targets))
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
		eligible = append(eligible, eligibleTarget{target: target, config: config})
	}

	// Phase 1: create/lookup per-target job rows first; collect the union of
	// symbols that still need a result this round.
	pending := make([][]pendingJob, len(eligible))
	dropped := make([]bool, len(eligible))
	neededSet := make(map[string]struct{})
	for i, et := range eligible {
		jobsForTarget := make([]pendingJob, 0, len(et.config.Symbols))
		for _, symbol := range et.config.Symbols {
			job := domain.Job{
				ID:          uuid.NewString(),
				TargetID:    et.target.ID,
				Symbol:      symbol,
				Interval:    period.Interval,
				PeriodStart: period.Start,
				PeriodEnd:   period.End,
				Status:      domain.JobPending,
			}
			created, isNew, err := e.jobs.CreateIfNotExists(ctx, job)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				dropped[i] = true
				break
			}
			if !isNew && created.Status == domain.JobSent {
				continue
			}
			jobsForTarget = append(jobsForTarget, pendingJob{job: created, symbol: symbol})
			neededSet[symbol] = struct{}{}
		}
		if !dropped[i] {
			pending[i] = jobsForTarget
		}
	}
	if len(neededSet) == 0 {
		return firstErr
	}

	// Phase 2: fetch each needed symbol ONCE, sequentially in sorted order.
	symbols := make([]string, 0, len(neededSet))
	for symbol := range neededSet {
		symbols = append(symbols, symbol)
	}
	slices.Sort(symbols)
	outcomes := make(map[string]fetchOutcome, len(symbols))
	for _, symbol := range symbols {
		candle, err := e.market.GetKline(ctx, symbol, period.Interval, period.Start, period.End)
		if err != nil {
			outcomes[symbol] = fetchOutcome{
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: symbol}, Unavailable: true},
			}
			continue
		}
		change, err := service.CalculateChange(candle, period.Interval, period)
		if err != nil {
			outcomes[symbol] = fetchOutcome{
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: symbol}, Unavailable: true},
			}
			continue
		}
		outcomes[symbol] = fetchOutcome{
			result: notification.PriceResult{Change: change},
		}
	}

	// Phase 3: deliver per target in original order from the shared map.
	for i, et := range eligible {
		if dropped[i] {
			continue
		}
		if err := e.deliverToTarget(ctx, et.target, period, pending[i], outcomes); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (e *TargetExecutor) deliverToTarget(ctx context.Context, target domain.AlertTarget, period domain.Period, pending []pendingJob, outcomes map[string]fetchOutcome) error {
	type item struct {
		job    domain.Job
		result notification.PriceResult
	}
	items := make([]item, 0, len(pending))
	for _, p := range pending {
		outcome, ok := outcomes[p.symbol]
		if !ok {
			outcome = fetchOutcome{
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: p.symbol}, Unavailable: true},
			}
		}
		items = append(items, item{
			job:    p.job,
			result: outcome.result,
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

// eligibleTarget pairs a target with its loaded alert configuration.
type eligibleTarget struct {
	target domain.AlertTarget
	config domain.AlertConfig
}

// pendingJob is a job row that still needs a result delivered this round.
type pendingJob struct {
	job    domain.Job
	symbol string
}

// fetchOutcome is the shared per-symbol result for one round.
type fetchOutcome struct {
	result notification.PriceResult
}

func containsInterval(values []domain.Interval, wanted domain.Interval) bool {
	return slices.Contains(values, wanted)
}
