package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"sync"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
)

var ErrFundingUnavailable = errors.New("funding service unavailable")

type FundingSettings struct {
	SnapshotTTL    time.Duration
	ExchangeTTL    time.Duration
	IntervalTTL    time.Duration
	RefreshTimeout time.Duration
}

func DefaultFundingSettings() FundingSettings {
	return FundingSettings{SnapshotTTL: 15 * time.Second, ExchangeTTL: 5 * time.Minute, IntervalTTL: time.Minute, RefreshTimeout: 15 * time.Second}
}

type fundingRefresh struct {
	done chan struct{}
	err  error
}

type FundingService struct {
	provider       market.FundingDataProvider
	settings       FundingSettings
	logger         *slog.Logger
	now            func() time.Time
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	flight         *fundingRefresh
	contracts      []domain.FundingContract
	contractsUntil time.Time
	intervals      map[string]int
	intervalsUntil time.Time
	rows           []domain.FundingRate
	invalid        int
	generated      time.Time
	until          time.Time
}

func NewFundingService(provider market.FundingDataProvider, settings FundingSettings, logger *slog.Logger) (*FundingService, error) {
	if provider == nil || settings.SnapshotTTL <= 0 || settings.ExchangeTTL <= 0 || settings.IntervalTTL <= 0 || settings.RefreshTimeout <= 0 {
		return nil, fmt.Errorf("invalid funding service settings")
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &FundingService{provider: provider, settings: settings, logger: logger, now: time.Now, ctx: ctx, cancel: cancel}, nil
}

// Close cancels any shared refresh during application shutdown.
func (s *FundingService) Close() {
	s.cancel()
}

// Get the funding rates, with a limit on the number of results returned.
func (s *FundingService) FundingRates(ctx context.Context, limit int) (domain.FundingRateResult, error) {
	if limit < domain.DefaultFundingLimit || limit > domain.MaxFundingLimit {
		return domain.FundingRateResult{}, fmt.Errorf("limit must be between 5 and 20")
	}
	if err := ctx.Err(); err != nil {
		return domain.FundingRateResult{}, err
	}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return domain.FundingRateResult{}, ErrFundingUnavailable
	}
	if s.now().Before(s.until) {
		result := s.selectResult(limit)
		s.mu.Unlock()
		s.logger.Debug("funding snapshot", "cache_hit", true, "returned_count", result.ReturnedCount)
		return result, nil
	}
	f := s.flight
	if f == nil {
		f = &fundingRefresh{done: make(chan struct{})}
		s.flight = f
		go s.refresh(f)
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return domain.FundingRateResult{}, ctx.Err()
	case <-f.done:
	}
	if ctx.Err() != nil {
		return domain.FundingRateResult{}, ctx.Err()
	}
	if f.err != nil {
		return domain.FundingRateResult{}, f.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return domain.FundingRateResult{}, ErrFundingUnavailable
	}
	if !s.now().Before(s.until) {
		return domain.FundingRateResult{}, &market.FundingUpstreamError{Status: 502}
	}
	return s.selectResult(limit), nil
}

// selectResult is called with mu held and returns independent response storage.
func (s *FundingService) selectResult(limit int) domain.FundingRateResult {
	n := min(limit, len(s.rows))
	rows := make([]domain.FundingRate, n)
	copy(rows, s.rows[:n])
	var oldest *time.Time
	for _, row := range rows {
		if oldest == nil || row.ObservedAt.Before(*oldest) {
			t := row.ObservedAt
			oldest = &t
		}
	}
	return domain.FundingRateResult{
		Source:                "binance_usdm",
		RateSourceField:       "lastFundingRate",
		RequestedLimit:        limit,
		ReturnedCount:         n,
		EligiblePositiveCount: len(s.rows),
		Shortfall:             n < limit,
		GeneratedAt:           s.generated,
		ObservedAt:            oldest,
		ExcludedInvalidCount:  s.invalid,
		Data:                  rows,
	}
}

func (s *FundingService) refresh(f *fundingRefresh) {
	start := s.now()
	ctx, cancel := context.WithTimeout(s.ctx, s.settings.RefreshTimeout)
	defer cancel()
	err := s.load(ctx)
	s.mu.Lock()
	f.err = err
	s.flight = nil
	close(f.done)
	s.mu.Unlock()
	if err != nil {
		s.logger.Warn("funding refresh failed", "duration", s.now().Sub(start), "error", err)
	}
}

func (s *FundingService) load(ctx context.Context) error {
	s.mu.Lock()
	contracts, contractUntil := s.contracts, s.contractsUntil
	intervals, intervalUntil := s.intervals, s.intervalsUntil
	s.mu.Unlock()
	var err error
	if !s.now().Before(contractUntil) {
		contracts, err = s.provider.FundingContracts(ctx)
		if err != nil {
			return err
		}
		contractUntil = s.now().Add(s.settings.ExchangeTTL)
	}
	if !s.now().Before(intervalUntil) {
		intervals, err = s.provider.FundingIntervals(ctx)
		if err != nil {
			return err
		}
		if intervals == nil {
			return &market.FundingUpstreamError{Status: 502}
		}
		intervalUntil = s.now().Add(s.settings.IntervalTTL)
	}
	observations, err := s.provider.FundingObservations(ctx)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	now := s.now().UTC()
	rows, invalid, err := rankFunding(contracts, intervals, observations, now)
	if err != nil {
		return err
	}
	until := now.Add(s.settings.SnapshotTTL)
	if contractUntil.Before(until) {
		until = contractUntil
	}
	if intervalUntil.Before(until) {
		until = intervalUntil
	}
	for _, row := range rows {
		if row.NextFundingTime.Before(until) {
			until = row.NextFundingTime
		}
		freshUntil := row.ObservedAt.Add(2 * time.Minute)
		if freshUntil.Before(until) {
			until = freshUntil
		}
	}
	if !until.After(now) {
		return &market.FundingUpstreamError{Status: 502}
	}
	s.mu.Lock()
	s.contracts, s.contractsUntil = contracts, contractUntil
	s.intervals, s.intervalsUntil = intervals, intervalUntil
	s.rows, s.invalid, s.generated, s.until = rows, invalid, now, until
	s.mu.Unlock()
	s.logger.Info("funding refresh", "cache_hit", false, "positive_count", len(rows), "excluded_invalid_count", invalid)
	return nil
}

func rankFunding(contracts []domain.FundingContract, intervals map[string]int, observations []domain.FundingObservation, now time.Time) ([]domain.FundingRate, int, error) {
	bad := func() ([]domain.FundingRate, int, error) { return nil, 0, &market.FundingUpstreamError{Status: 502} }
	bySymbol := make(map[string]domain.FundingContract, len(contracts))

	for _, c := range contracts {
		if c.Validate() != nil {
			return bad()
		}
		if _, exists := bySymbol[c.Symbol]; exists {
			return bad()
		}
		bySymbol[c.Symbol] = c
	}
	for symbol, hours := range intervals {
		if symbol == "" || hours <= 0 || hours > 24 {
			return bad()
		}
	}
	rows := make([]domain.FundingRate, 0)
	values := make(map[string]*big.Rat)
	seen := map[string]domain.FundingObservation{}
	invalid, valid := 0, 0
	for _, o := range observations {
		if old, exists := seen[o.Symbol]; exists {
			if old != o {
				return bad()
			}
			continue
		}
		seen[o.Symbol] = o
		c, eligible := bySymbol[o.Symbol]
		if !eligible {
			if o.Symbol == "" {
				invalid++
			}
			continue
		}
		if o.Validate(now) != nil {
			invalid++
			continue
		}
		valid++
		n, _ := domain.ParseFundingDecimal(o.Rate)
		if n.Sign() <= 0 {
			continue
		}
		hours := 8 // Official default; only used after successful interval retrieval.
		if override, ok := intervals[o.Symbol]; ok {
			hours = override
		}
		rows = append(rows, domain.FundingRate{
			Symbol:          c.Symbol,
			BaseAsset:       c.BaseAsset,
			QuoteAsset:      c.QuoteAsset,
			Rate:            o.Rate,
			IntervalHours:   hours,
			NextFundingTime: o.NextFundingTime.UTC(),
			ObservedAt:      o.ObservedAt.UTC(),
		})
		values[o.Symbol] = n
	}
	for symbol := range bySymbol {
		if _, ok := seen[symbol]; !ok {
			invalid++
		}
	}
	if len(contracts) > 0 && valid == 0 {
		return bad()
	}
	sort.Slice(rows, func(i, j int) bool {
		n := values[rows[i].Symbol].Cmp(values[rows[j].Symbol])
		if n == 0 {
			return rows[i].Symbol < rows[j].Symbol
		}
		return n > 0
	})
	return rows, invalid, nil
}
