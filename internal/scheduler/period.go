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
	if e == nil || e.location == nil || interval.Validate() != nil {
		return domain.Period{}, false
	}
	localNow := now.In(e.location)
	date := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, e.location)
	minute := localNow.Hour()*60 + localNow.Minute()
	if minute < 6*60 || minute >= 23*60 {
		return domain.Period{}, false
	}

	var start, end time.Time
	switch interval {
	case domain.Interval1H:
		if localNow.Minute() != 0 {
			return domain.Period{}, false
		}
		end = time.Date(date.Year(), date.Month(), date.Day(), localNow.Hour(), 0, 0, 0, e.location)
		start = end.Add(-time.Hour)
		if start.Hour() < 6 {
			return domain.Period{}, false
		}
	case domain.Interval4H:
		switch localNow.Hour() {
		case 10:
			start = time.Date(date.Year(), date.Month(), date.Day(), 6, 0, 0, 0, e.location)
		case 14:
			start = time.Date(date.Year(), date.Month(), date.Day(), 10, 0, 0, 0, e.location)
		case 18:
			start = time.Date(date.Year(), date.Month(), date.Day(), 14, 0, 0, 0, e.location)
		case 22:
			start = time.Date(date.Year(), date.Month(), date.Day(), 18, 0, 0, 0, e.location)
		default:
			return domain.Period{}, false
		}
		if localNow.Minute() != 0 {
			return domain.Period{}, false
		}
		end = time.Date(date.Year(), date.Month(), date.Day(), localNow.Hour(), 0, 0, 0, e.location)
	}
	return domain.Period{Interval: interval, Start: start, End: end}, true
}
