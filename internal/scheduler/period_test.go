package scheduler

import (
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func newTestEngine(t *testing.T) *PeriodEngine {
	t.Helper()
	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewPeriodEngine(location)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestPeriodEngineOneHourBoundaries(t *testing.T) {
	engine := newTestEngine(t)
	location := engine.location
	tests := []struct {
		name  string
		hour  int
		valid bool
	}{
		{"before active", 5, false},
		{"first active trigger", 6, false},
		{"normal active", 10, true},
		{"last active trigger", 22, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 9, 1, tt.hour, 0, 0, 0, location)
			period, ok := engine.GetCurrentPeriod(now, domain.Interval1H)
			if ok != tt.valid {
				t.Fatalf("valid=%v, want %v; period=%+v", ok, tt.valid, period)
			}
		})
	}
}

func TestPeriodEngineFourHourWindows(t *testing.T) {
	engine := newTestEngine(t)
	location := engine.location
	for _, hour := range []int{11, 15, 19, 23} {
		period, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, hour, 0, 0, 0, location), domain.Interval4H)
		if !ok || period.End.Hour() != hour || period.End.Sub(period.Start) != 4*time.Hour {
			t.Fatalf("hour %d returned %+v, valid=%v", hour, period, ok)
		}
	}
	for _, hour := range []int{10, 14, 18, 22, 2} {
		if _, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, hour, 0, 0, 0, location), domain.Interval4H); ok {
			t.Fatalf("hour %d should not produce a 4H period", hour)
		}
	}
}

func TestPeriodEngineFourHourWindowsUseUTCAlignedBoundaries(t *testing.T) {
	engine := newTestEngine(t)
	location := engine.location
	tests := []struct {
		endHour   int
		startHour int
	}{
		{endHour: 11, startHour: 7},
		{endHour: 15, startHour: 11},
		{endHour: 19, startHour: 15},
		{endHour: 23, startHour: 19},
	}
	for _, tt := range tests {
		period, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, tt.endHour, 0, 0, 0, location), domain.Interval4H)
		if !ok || period.Start.Hour() != tt.startHour || period.End.Hour() != tt.endHour {
			t.Fatalf("end hour %d returned %+v, valid=%v", tt.endHour, period, ok)
		}
		if period.Start.UTC().Hour() != (tt.startHour-7+24)%24 || period.End.UTC().Hour() != (tt.endHour-7+24)%24 {
			t.Fatalf("period %+v is not UTC aligned", period)
		}
	}
}

func TestPeriodEngineRequiresExactHour(t *testing.T) {
	engine := newTestEngine(t)
	location := engine.location
	if _, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, 10, 30, 0, 0, location), domain.Interval1H); ok {
		t.Fatal("expected no period away from the hour boundary")
	}
}

func TestPeriodEngineFifteenMinuteAndTwoHourWindows(t *testing.T) {
	engine := newTestEngine(t)
	location := engine.location

	// 15m tests
	p15, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, 10, 15, 0, 0, location), domain.Interval15M)
	if !ok || p15.End.Sub(p15.Start) != 15*time.Minute {
		t.Fatalf("expected valid 15m period, got ok=%v, period=%+v", ok, p15)
	}

	// 15m off-boundary
	if _, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, 10, 16, 0, 0, location), domain.Interval15M); ok {
		t.Fatal("expected off-boundary 15m to fail")
	}

	// 2h tests (Asia/Ho_Chi_Minh: 07:00, 09:00, 11:00...)
	p2h, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, 9, 0, 0, 0, location), domain.Interval2H)
	if !ok || p2h.End.Sub(p2h.Start) != 2*time.Hour || p2h.Start.Hour() != 7 {
		t.Fatalf("expected valid 2h period (07:00 -> 09:00), got ok=%v, period=%+v", ok, p2h)
	}
}
