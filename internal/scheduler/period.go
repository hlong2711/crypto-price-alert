package scheduler

import (
	"fmt"
	"time"

	"crypto-price-alert/internal/domain"
)

type PeriodEngine struct {
	location *time.Location
}

func NewPeriodEngine(location *time.Location) (*PeriodEngine, error) {
	if location == nil {
		return nil, fmt.Errorf("location is required")
	}
	return &PeriodEngine{location: location}, nil
}

// GetCurrentPeriod returns the notification period that ended at now.
func (e *PeriodEngine) GetCurrentPeriod(now time.Time, interval domain.Interval) (domain.Period, bool) {
	if e == nil || e.location == nil {
		return domain.Period{}, false
	}
	dur, err := interval.Duration()
	if err != nil {
		return domain.Period{}, false
	}

	localNow := now.In(e.location).Truncate(time.Minute)
	end := localNow
	start := end.Add(-dur)

	utcEnd := end.UTC()
	utcMidnight := time.Date(utcEnd.Year(), utcEnd.Month(), utcEnd.Day(), 0, 0, 0, 0, time.UTC)
	elapsed := utcEnd.Sub(utcMidnight)
	if elapsed < 0 || elapsed%dur != 0 {
		return domain.Period{}, false
	}

	startLocal := start.In(e.location)
	endLocal := end.In(e.location)
	baseDate := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, e.location)

	startM := int(startLocal.Sub(baseDate).Minutes())
	endM := int(endLocal.Sub(baseDate).Minutes())

	activeFromM := 6 * 60
	activeUntilM := 23 * 60

	// out of working range
	if startM < activeFromM || endM > activeUntilM {
		return domain.Period{}, false
	}

	return domain.Period{Interval: interval, Start: start, End: end}, true
}
