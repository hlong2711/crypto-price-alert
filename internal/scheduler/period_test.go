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
	for _, hour := range []int{10, 14, 18, 22} {
		period, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, hour, 0, 0, 0, location), domain.Interval4H)
		if !ok || period.End.Hour() != hour || period.End.Sub(period.Start) != 4*time.Hour {
			t.Fatalf("hour %d returned %+v, valid=%v", hour, period, ok)
		}
	}
	for _, hour := range []int{9, 23, 2} {
		if _, ok := engine.GetCurrentPeriod(time.Date(2026, 9, 1, hour, 0, 0, 0, location), domain.Interval4H); ok {
			t.Fatalf("hour %d should not produce a 4H period", hour)
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
