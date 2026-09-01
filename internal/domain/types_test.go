package domain

import (
	"testing"
	"time"
)

func TestDomainValidation(t *testing.T) {
	if err := Interval1H.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := Interval("15m").Validate(); err == nil {
		t.Fatal("expected invalid interval")
	}
	now := time.Now()
	job := Job{ID: "job-1", Symbol: "BTCUSDT", Interval: Interval1H, PeriodStart: now, PeriodEnd: now.Add(time.Hour), Status: JobPending}
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Candle{Symbol: "BTCUSDT", Open: 100, High: 110, Low: 90, Close: 105, OpenTime: now, CloseTime: now.Add(time.Hour)}).Validate(); err != nil {
		t.Fatal(err)
	}
}
