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

	var start, end time.Time
	switch interval {
	case domain.Interval1H:
		if minute < 6*60 || minute >= 23*60 {
			return domain.Period{}, false
		}
		if localNow.Minute() != 0 {
			return domain.Period{}, false
		}
		end = time.Date(date.Year(), date.Month(), date.Day(), localNow.Hour(), 0, 0, 0, e.location)
		start = end.Add(-time.Hour)
		if start.Hour() < 6 {
			return domain.Period{}, false
		}
	case domain.Interval4H:
		// Binance 4h candles are aligned to UTC. In Asia/Ho_Chi_Minh,
		// those boundaries are 07:00, 11:00, 15:00, 19:00, and 23:00.
		if minute < 7*60 || minute > 23*60 {
			return domain.Period{}, false
		}
		switch localNow.Hour() {
		case 11:
			start = time.Date(date.Year(), date.Month(), date.Day(), 7, 0, 0, 0, e.location)
		case 15:
			start = time.Date(date.Year(), date.Month(), date.Day(), 11, 0, 0, 0, e.location)
		case 19:
			start = time.Date(date.Year(), date.Month(), date.Day(), 15, 0, 0, 0, e.location)
		case 23:
			start = time.Date(date.Year(), date.Month(), date.Day(), 19, 0, 0, 0, e.location)
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
