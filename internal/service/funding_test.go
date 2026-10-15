package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

type fundingFake struct {
	contracts    []domain.FundingContract
	intervals    map[string]int
	observations []domain.FundingObservation
	calls        [3]atomic.Int32
	err          error
	gate         chan struct{}
	started      chan struct{}
}

func (f *fundingFake) FundingContracts(context.Context) ([]domain.FundingContract, error) {
	f.calls[0].Add(1)
	return f.contracts, f.err
}
func (f *fundingFake) FundingIntervals(context.Context) (map[string]int, error) {
	f.calls[1].Add(1)
	return f.intervals, f.err
}
func (f *fundingFake) FundingObservations(ctx context.Context) ([]domain.FundingObservation, error) {
	f.calls[2].Add(1)
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.gate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.gate:
		}
	}
	return f.observations, f.err
}
func fundingFixture(now time.Time, n int) *fundingFake {
	f := &fundingFake{intervals: map[string]int{"C00USDT": 4}}
	for i := 0; i < n; i++ {
		symbol := fmt.Sprintf("C%02dUSDT", i)
		f.contracts = append(f.contracts, domain.FundingContract{Symbol: symbol, BaseAsset: fmt.Sprintf("C%02d", i), QuoteAsset: "USDT"})
		f.observations = append(f.observations, domain.FundingObservation{Symbol: symbol, Rate: fmt.Sprintf("0.%03d", i+1), ObservedAt: now, NextFundingTime: now.Add(time.Hour)})
	}
	return f
}
func fundingServiceForTest(t *testing.T, f *fundingFake, now *time.Time) *FundingService {
	t.Helper()
	s, err := NewFundingService(f, DefaultFundingSettings(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return *now }
	t.Cleanup(s.Close)
	return s
}

func TestFundingRankingAndResults(t *testing.T) {
	now := time.Now().UTC()
	f := fundingFixture(now, 8)
	f.contracts[7].QuoteAsset = "USDC" // Metadata, not suffix, defines eligibility.
	f.observations[0].Rate = "0"
	f.observations[1].Rate = "-0.1"
	f.observations[2].Rate = "0.008" // Tie resolves C02 before C07.
	s := fundingServiceForTest(t, f, &now)
	r, err := s.FundingRates(context.Background(), 5)
	if err != nil || r.ReturnedCount != 5 || r.EligiblePositiveCount != 6 || r.Shortfall {
		t.Fatal(r, err)
	}
	if r.Data[0].Symbol != "C02USDT" || r.Data[1].Symbol != "C07USDT" || r.Data[1].QuoteAsset != "USDC" {
		t.Fatal(r.Data)
	}
	r.Data[0].Rate = "mutated"
	r, err = s.FundingRates(context.Background(), 20)
	if err != nil || r.ReturnedCount != 6 || !r.Shortfall || r.Data[0].Rate == "mutated" {
		t.Fatal(r, err)
	}
	if f.calls[0].Load() != 1 || f.calls[1].Load() != 1 || f.calls[2].Load() != 1 {
		t.Fatal("warm request fetched upstream")
	}
	if _, err = s.FundingRates(context.Background(), 4); err == nil {
		t.Fatal("invalid limit accepted")
	}
}

func TestFundingIntervalsShortfallAndEmpty(t *testing.T) {
	now := time.Now().UTC()
	f := fundingFixture(now, 2)
	s := fundingServiceForTest(t, f, &now)
	r, err := s.FundingRates(context.Background(), 5)
	if err != nil || !r.Shortfall || r.Data[1].IntervalHours != 4 || r.Data[0].IntervalHours != 8 {
		t.Fatal(r, err)
	}
	f2 := fundingFixture(now, 2)
	for i := range f2.observations {
		f2.observations[i].Rate = "0"
	}
	s2 := fundingServiceForTest(t, f2, &now)
	r, err = s2.FundingRates(context.Background(), 5)
	if err != nil || r.Data == nil || len(r.Data) != 0 || r.ObservedAt != nil || !r.Shortfall {
		t.Fatal(r, err)
	}
}

func TestFundingInvalidRowsAndSnapshot(t *testing.T) {
	now := time.Now().UTC()
	f := fundingFixture(now, 8)
	f.observations[0].Rate = "NaN"
	f.observations[1].ObservedAt = now.Add(-3 * time.Minute)
	s := fundingServiceForTest(t, f, &now)
	r, err := s.FundingRates(context.Background(), 5)
	if err != nil || r.ReturnedCount != 5 || r.ExcludedInvalidCount != 2 {
		t.Fatal(r, err)
	}
	for _, mutate := range []func(*fundingFake){
		func(f *fundingFake) { f.observations = nil },
		func(f *fundingFake) {
			for i := range f.observations {
				f.observations[i].Rate = "invalid"
			}
		},
		func(f *fundingFake) { f.intervals = nil },
		func(f *fundingFake) { f.intervals["X"] = 0 },
		func(f *fundingFake) {
			f.observations = append(f.observations, domain.FundingObservation{Symbol: f.observations[0].Symbol, Rate: "different"})
		},
	} {
		bad := fundingFixture(now, 2)
		mutate(bad)
		s := fundingServiceForTest(t, bad, &now)
		if _, err := s.FundingRates(context.Background(), 5); err == nil {
			t.Fatal("corrupt snapshot accepted")
		}
	}
}

func TestFundingCacheExpiryAndFailure(t *testing.T) {
	now := time.Now().UTC()
	f := fundingFixture(now, 6)
	s := fundingServiceForTest(t, f, &now)
	if _, err := s.FundingRates(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Second)
	if _, err := s.FundingRates(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].Load() != 1 || f.calls[1].Load() != 1 || f.calls[2].Load() != 2 {
		t.Fatal("metadata not cached")
	}
	now = now.Add(time.Minute)
	f.err = errors.New("upstream failed")
	if _, err := s.FundingRates(context.Background(), 5); err == nil {
		t.Fatal("served stale snapshot")
	}
	f.err = nil
	if _, err := s.FundingRates(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if f.calls[1].Load() != 3 {
		t.Fatal("interval metadata did not expire")
	}
	now = now.Add(5 * time.Minute)
	for i := range f.observations {
		f.observations[i].ObservedAt = now
		f.observations[i].NextFundingTime = now.Add(time.Hour)
	}
	if _, err := s.FundingRates(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].Load() != 2 {
		t.Fatal("exchange metadata did not expire")
	}
	s.Close()
	if _, err := s.FundingRates(context.Background(), 5); !errors.Is(err, ErrFundingUnavailable) {
		t.Fatal(err)
	}
}

func TestFundingBoundaryExpiry(t *testing.T) {
	now := time.Now().UTC()
	f := fundingFixture(now, 6)
	f.observations[0].NextFundingTime = now.Add(time.Second)
	s := fundingServiceForTest(t, f, &now)
	if _, err := s.FundingRates(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	// Even the sixth ranked pair bounds the full snapshot expiry.
	now = now.Add(2 * time.Second)
	r, err := s.FundingRates(context.Background(), 5)
	if err != nil || f.calls[2].Load() != 2 || r.EligiblePositiveCount != 5 || r.ExcludedInvalidCount != 1 {
		t.Fatal(r, err)
	}
}

func TestFundingCoalescingAndWaiterCancellation(t *testing.T) {
	now := time.Now().UTC()
	f := fundingFixture(now, 8)
	f.gate = make(chan struct{})
	f.started = make(chan struct{}, 1)
	s := fundingServiceForTest(t, f, &now)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := s.FundingRates(ctx, 5); first <- err }()
	<-f.started
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, err := s.FundingRates(context.Background(), 5+i); errs <- err }(i)
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(f.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.calls[2].Load() != 1 {
		t.Fatalf("%d duplicate refreshes", f.calls[2].Load())
	}
}

func TestFundingRefreshTimeoutAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		now := time.Now().UTC()
		f := fundingFixture(now, 5)
		f.gate = make(chan struct{})
		f.started = make(chan struct{}, 1)
		s := fundingServiceForTest(t, f, &now)
		s.settings.RefreshTimeout = 25 * time.Millisecond
		result := make(chan error, 1)
		go func() { _, err := s.FundingRates(context.Background(), 5); result <- err }()
		<-f.started
		if shutdown {
			s.Close()
		}
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("expected cancellation/deadline")
			}
		case <-time.After(time.Second):
			t.Fatal("refresh leaked")
		}
	}
}
