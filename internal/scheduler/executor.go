package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/notification"
	"crypto-price-alert/internal/repository"
	"github.com/google/uuid"
)

type Executor struct {
	periods    *PeriodEngine
	market     market.MarketDataProvider
	repository repository.JobRepository
	notifiers  []notification.Notifier
	symbols    []string
	location   *time.Location
}

func NewExecutor(periods *PeriodEngine, provider market.MarketDataProvider, jobs repository.JobRepository, notifiers []notification.Notifier, symbols []string, location *time.Location) (*Executor, error) {
	if periods == nil || provider == nil || jobs == nil || len(notifiers) == 0 || len(symbols) == 0 || location == nil {
		return nil, fmt.Errorf("invalid executor settings")
	}
	return &Executor{periods: periods, market: provider, repository: jobs, notifiers: notifiers, symbols: symbols, location: location}, nil
}

func (e *Executor) Execute(ctx context.Context, now time.Time, interval domain.Interval) error {
	period, ok := e.periods.GetCurrentPeriod(now, interval)
	if !ok {
		return nil
	}
	type item struct {
		symbol string
		job    domain.Job
		result notification.PriceResult
	}
	items := make([]item, 0, len(e.symbols))
	for _, symbol := range e.symbols {
		job := domain.Job{ID: uuid.New().String(), Symbol: symbol, Interval: interval, PeriodStart: period.Start, PeriodEnd: period.End, Status: domain.JobPending}
		created, isNew, err := e.repository.CreateIfNotExists(ctx, job)
		if err != nil {
			return err
		}
		if !isNew && created.Status == domain.JobSent {
			continue
		}
		candle, err := e.market.GetKline(ctx, symbol, interval, period.Start, period.End)
		if err != nil {
			items = append(items, item{symbol: symbol, job: created, result: notification.PriceResult{Change: domain.PriceChange{Symbol: symbol}, Unavailable: true}})
			continue
		}
		change, err := calculateChange(candle, interval, period)
		if err != nil {
			items = append(items, item{symbol: symbol, job: created, result: notification.PriceResult{Change: domain.PriceChange{Symbol: symbol}, Unavailable: true}})
			continue
		}
		items = append(items, item{symbol: symbol, job: created, result: notification.PriceResult{Change: change}})
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
		if err := notifier.Send(ctx, message); err != nil {
			sendErr = err
		}
	}
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sendErr != nil {
				_ = e.repository.MarkFailed(ctx, item.job.ID, sendErr.Error())
			} else {
				_ = e.repository.MarkSent(ctx, item.job.ID, time.Now().UTC())
			}
		}()
	}
	wg.Wait()
	return sendErr
}

func calculateChange(candle domain.Candle, interval domain.Interval, period domain.Period) (domain.PriceChange, error) {
	if err := candle.Validate(); err != nil {
		return domain.PriceChange{}, err
	}
	if candle.OpenTime.Before(period.Start) || candle.CloseTime.After(period.End.Add(time.Second)) {
		return domain.PriceChange{}, fmt.Errorf("candle does not match period")
	}
	return domain.PriceChange{Symbol: candle.Symbol, Interval: interval, Open: candle.Open, Close: candle.Close, ChangePct: (candle.Close - candle.Open) / candle.Open * 100, StartTime: period.Start, EndTime: period.End}, nil
}
