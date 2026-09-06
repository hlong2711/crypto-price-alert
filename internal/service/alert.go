package service

import (
	"context"
	"fmt"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/notification"
)

// SymbolResult is the per-symbol outcome of a run.
type SymbolResult struct {
	Symbol      string  `json:"symbol"`
	Open        float64 `json:"open"`
	Close       float64 `json:"close"`
	ChangePct   float64 `json:"change_pct"`
	Volume      float64 `json:"volume"`
	Unavailable bool    `json:"unavailable"`
	Error       string  `json:"error,omitempty"`
}

// RunResult is returned to API callers.
type RunResult struct {
	Interval       domain.Interval   `json:"interval"`
	PeriodStart    time.Time         `json:"period_start"`
	PeriodEnd      time.Time         `json:"period_end"`
	Results        []SymbolResult    `json:"results"`
	MessagePreview string            `json:"message_preview"`
	DryRun         bool              `json:"dry_run"`
	Sent           map[string]string `json:"sent,omitempty"`
}

// AlertService runs the core job logic: fetch klines, build message, send notifications.
// It intentionally does NOT touch the scheduler or the job repository.
type AlertService struct {
	market         market.MarketDataProvider
	notifiers      []notification.Notifier
	notifierNames  []string
	defaultSymbols []string
	location       *time.Location
}

func NewAlertService(
	provider market.MarketDataProvider,
	notifiers []notification.Notifier,
	notifierNames []string,
	defaultSymbols []string,
	location *time.Location,
) (*AlertService, error) {
	if provider == nil || len(notifiers) == 0 || len(defaultSymbols) == 0 || location == nil {
		return nil, fmt.Errorf("invalid alert service settings")
	}
	if len(notifierNames) != 0 && len(notifierNames) != len(notifiers) {
		return nil, fmt.Errorf("notifier names length must match notifiers")
	}
	return &AlertService{
		market:         provider,
		notifiers:      notifiers,
		notifierNames:  notifierNames,
		defaultSymbols: defaultSymbols,
		location:       location,
	}, nil
}

func (s *AlertService) DefaultSymbols() []string {
	out := make([]string, len(s.defaultSymbols))
	copy(out, s.defaultSymbols)
	return out
}

// Run fetches klines for period, builds the message and sends it unless dryRun.
func (s *AlertService) Run(ctx context.Context, interval domain.Interval, period domain.Period, symbols []string, dryRun bool) (RunResult, error) {
	if err := interval.Validate(); err != nil {
		return RunResult{}, err
	}
	if err := period.Validate(); err != nil {
		return RunResult{}, fmt.Errorf("invalid period: %w", err)
	}
	if len(symbols) == 0 {
		symbols = s.DefaultSymbols()
	}

	priceResults := make([]notification.PriceResult, 0, len(symbols))
	symbolResults := make([]SymbolResult, 0, len(symbols))
	for _, symbol := range symbols {
		candle, err := s.market.GetKline(ctx, symbol, interval, period.Start, period.End)
		if err != nil {
			priceResults = append(priceResults, notification.PriceResult{
				Change:      domain.PriceChange{Symbol: symbol},
				Unavailable: true,
			})
			symbolResults = append(symbolResults, SymbolResult{Symbol: symbol, Unavailable: true, Error: err.Error()})
			continue
		}
		change, err := CalculateChange(candle, interval, period)
		if err != nil {
			priceResults = append(priceResults, notification.PriceResult{
				Change:      domain.PriceChange{Symbol: symbol},
				Unavailable: true,
			})
			symbolResults = append(symbolResults, SymbolResult{Symbol: symbol, Unavailable: true, Error: err.Error()})
			continue
		}
		priceResults = append(priceResults, notification.PriceResult{Change: change})
		symbolResults = append(symbolResults, SymbolResult{
			Symbol:    symbol,
			Open:      change.Open,
			Close:     change.Close,
			ChangePct: change.ChangePct,
			Volume:    change.Volume,
		})
	}
	if len(priceResults) == 0 {
		return RunResult{}, fmt.Errorf("no symbols to process")
	}

	message, err := notification.BuildMessage(period, priceResults, s.location)
	if err != nil {
		return RunResult{}, err
	}
	preview := notification.RenderMessage(message)

	result := RunResult{
		Interval:       interval,
		PeriodStart:    period.Start,
		PeriodEnd:      period.End,
		Results:        symbolResults,
		MessagePreview: preview,
		DryRun:         dryRun,
	}
	if dryRun {
		return result, nil
	}

	sent := make(map[string]string, len(s.notifiers))
	for i, notifier := range s.notifiers {
		name := fmt.Sprintf("notifier_%d", i)
		if i < len(s.notifierNames) {
			name = s.notifierNames[i]
		}
		if err := notifier.Send(ctx, message); err != nil {
			sent[name] = "failed: " + err.Error()
		} else {
			sent[name] = "sent"
		}
	}
	result.Sent = sent
	return result, nil
}

func CalculateChange(candle domain.Candle, interval domain.Interval, period domain.Period) (domain.PriceChange, error) {
	if err := candle.Validate(); err != nil {
		return domain.PriceChange{}, err
	}
	if candle.OpenTime.Before(period.Start) || candle.CloseTime.After(period.End.Add(time.Second)) {
		return domain.PriceChange{}, fmt.Errorf("candle does not match period")
	}
	return domain.PriceChange{
		Symbol:    candle.Symbol,
		Interval:  interval,
		Open:      candle.Open,
		Close:     candle.Close,
		ChangePct: (candle.Close - candle.Open) / candle.Open * 100,
		Volume:    candle.Volume,
		StartTime: period.Start,
		EndTime:   period.End,
	}, nil
}
