package service

import (
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func TestCalculateChangePropagatesVolume(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	period := domain.Period{
		Interval: domain.Interval1H,
		Start:    start,
		End:      start.Add(time.Hour),
	}
	candle := domain.Candle{
		Symbol:    "BTCUSDT",
		Open:      100,
		High:      110,
		Low:       90,
		Close:     105,
		Volume:    1234.5,
		OpenTime:  start,
		CloseTime: period.End,
	}

	change, err := CalculateChange(candle, domain.Interval1H, period)
	if err != nil {
		t.Fatal(err)
	}
	if change.Volume != candle.Volume {
		t.Fatalf("volume=%v, want %v", change.Volume, candle.Volume)
	}
}
