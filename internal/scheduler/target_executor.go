package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
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
	providers market.ProviderResolver
	jobs      repository.JobRepository
	targets   repository.AlertTargetRepository
	configs   repository.AlertConfigRepository
	notifiers []notification.Notifier
	legacy    *Executor
	location  *time.Location
	mu        sync.Mutex
	logger    *slog.Logger
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
	return NewTargetExecutorWithResolver(periods, market.NewStaticProviderResolver(provider), jobs, targets, configs, notifiers, legacy, location)
}

func NewTargetExecutorWithResolver(
	periods *PeriodEngine,
	providers market.ProviderResolver,
	jobs repository.JobRepository,
	targets repository.AlertTargetRepository,
	configs repository.AlertConfigRepository,
	notifiers []notification.Notifier,
	legacy *Executor,
	location *time.Location,
) (*TargetExecutor, error) {
	if periods == nil || providers == nil || jobs == nil || targets == nil || configs == nil || len(notifiers) == 0 || location == nil {
		return nil, fmt.Errorf("invalid target executor settings")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return &TargetExecutor{
		periods:   periods,
		providers: providers,
		jobs:      jobs,
		targets:   targets,
		configs:   configs,
		notifiers: notifiers,
		legacy:    legacy,
		location:  location,
		logger:    logger,
	}, nil
}

// Execute loads targets and configurations at the period boundary.
func (e *TargetExecutor) Execute(ctx context.Context, now time.Time, interval domain.Interval) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	period, due := e.periods.GetCurrentPeriod(now, interval)
	if !due {
		return nil
	}

	targets, err := e.targets.ListEnabledTargets(ctx)
	if err != nil {
		return err
	}

	e.logger.Info("target execute for interval", "inteval", interval, "targetCount", len(targets))
	if len(targets) == 0 {
		if e.legacy == nil {
			return nil
		}
		return e.legacy.Execute(ctx, now, interval)
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
		if config.MarketProvider == "" {
			config.MarketProvider = domain.MarketProviderBinance
		}
		if !e.providers.Supports(config.MarketProvider, "", interval) {
			// Static compatibility resolvers cannot inspect symbols; the concrete
			// registry performs the full symbol/interval check below.
			if _, err := e.providers.Get(config.MarketProvider); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		eligible = append(eligible, eligibleTarget{target: target, config: config})
	}

	// Phase 1: create/lookup per-target job rows first; collect the union of
	// symbols that still need a result this round.
	pending := make([][]pendingJob, len(eligible))
	dropped := make([]bool, len(eligible))
	neededSet := make(map[fetchKey]struct{})
	for i, et := range eligible {
		jobsForTarget := make([]pendingJob, 0, len(et.config.Symbols))
		for _, symbol := range et.config.Symbols {
			if !e.providers.Supports(et.config.MarketProvider, symbol, interval) {
				if firstErr == nil {
					firstErr = fmt.Errorf("market provider %q does not support %s/%s", et.config.MarketProvider, symbol, interval)
				}
				dropped[i] = true
				break
			}
			job := domain.Job{
				ID:             uuid.NewString(),
				TargetID:       et.target.ID,
				MarketProvider: et.config.MarketProvider,
				Symbol:         symbol,
				Interval:       period.Interval,
				PeriodStart:    period.Start,
				PeriodEnd:      period.End,
				Status:         domain.JobPending,
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
			neededSet[fetchKey{provider: et.config.MarketProvider, symbol: symbol}] = struct{}{}
		}
		if !dropped[i] {
			pending[i] = jobsForTarget
		}
	}
	if len(neededSet) == 0 {
		return firstErr
	}

	// Phase 2: fetch each needed symbol ONCE, sequentially in sorted order.
	keys := make([]fetchKey, 0, len(neededSet))
	for key := range neededSet {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b fetchKey) int {
		if a.provider != b.provider {
			if a.provider < b.provider {
				return -1
			}
			return 1
		}
		return strings.Compare(a.symbol, b.symbol)
	})
	outcomes := make(map[fetchKey]fetchOutcome, len(keys))
	for _, key := range keys {
		provider, err := e.providers.Get(key.provider)
		if err != nil {
			outcomes[key] = fetchOutcome{result: notification.PriceResult{Change: domain.PriceChange{Symbol: key.symbol}, Unavailable: true}}
			continue
		}
		candle, err := provider.GetKline(ctx, key.symbol, period.Interval, period.Start, period.End)
		if err != nil {
			outcomes[key] = fetchOutcome{
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: key.symbol}, Unavailable: true},
			}
			continue
		}
		change, err := service.CalculateChange(candle, period.Interval, period)
		if err != nil {
			outcomes[key] = fetchOutcome{
				result: notification.PriceResult{Change: domain.PriceChange{Symbol: key.symbol}, Unavailable: true},
			}
			continue
		}
		outcomes[key] = fetchOutcome{
			result: notification.PriceResult{Change: change},
		}
	}

	e.logger.Info("target delivery to targets", "inteval", interval, "targetCount", len(eligible))
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

func (e *TargetExecutor) deliverToTarget(ctx context.Context, target domain.AlertTarget, period domain.Period, pending []pendingJob, outcomes map[fetchKey]fetchOutcome) error {
	type item struct {
		job    domain.Job
		result notification.PriceResult
	}
	items := make([]item, 0, len(pending))
	for _, p := range pending {
		outcome, ok := outcomes[fetchKey{provider: p.job.MarketProvider, symbol: p.symbol}]
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

type fetchKey struct {
	provider domain.MarketProvider
	symbol   string
}

func containsInterval(values []domain.Interval, wanted domain.Interval) bool {
	return slices.Contains(values, wanted)
}
