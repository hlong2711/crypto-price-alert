package domain

import (
	"testing"
	"time"
)

func TestDomainValidation(t *testing.T) {
	if err := Interval1H.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := Interval15M.Validate(); err != nil {
		t.Fatal(err)
	}
	if dur, err := Interval15M.Duration(); err != nil || dur != 15*time.Minute {
		t.Fatalf("expected 15m duration, got %v, err=%v", dur, err)
	}
	if err := Interval("10m").Validate(); err == nil {
		t.Fatal("expected invalid interval")
	}
	now := time.Now()
	job := Job{ID: "job-1", Symbol: "BTC", MarketProvider: MarketProviderBinance, Interval: Interval1H, PeriodStart: now, PeriodEnd: now.Add(time.Hour), Status: JobPending}
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Candle{Symbol: "BTCUSDT", Open: 100, High: 110, Low: 90, Close: 105, Volume: 1234, OpenTime: now, CloseTime: now.Add(time.Hour)}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Candle{Symbol: "BTCUSDT", Open: 100, High: 110, Low: 90, Close: 105, Volume: -1, OpenTime: now, CloseTime: now.Add(time.Hour)}).Validate(); err == nil {
		t.Fatal("expected negative volume to be rejected")
	}
}

func TestMarketProviderValidation(t *testing.T) {
	for _, provider := range []MarketProvider{MarketProviderBinance, MarketProviderCoinMarketCap} {
		if err := provider.Validate(); err != nil {
			t.Fatalf("expected %q to be valid: %v", provider, err)
		}
	}
	if err := MarketProvider("unknown").Validate(); err == nil {
		t.Fatal("expected unknown market provider to be rejected")
	}
}
