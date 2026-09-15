package notification

import (
	"strings"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func testPeriod() domain.Period {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, location)
	return domain.Period{Interval: domain.Interval1H, Start: start, End: start.Add(time.Hour)}
}

func TestBuildMessageAggregatesAndFormats(t *testing.T) {
	message, err := BuildMessage(testPeriod(), []PriceResult{
		{Change: domain.PriceChange{Symbol: "BTCUSDT", Close: 108420, ChangePct: 1.82, Volume: 148976.11427815}},
		{Change: domain.PriceChange{Symbol: "SOLUSDT", Close: 198.32, ChangePct: -2.13, Volume: 52340.25}},
	}, testPeriod().Start.Location())
	if err != nil {
		t.Fatal(err)
	}
	rendered := RenderMessage(message)
	for _, expected := range []string{"📊 Crypto 1h Update", "09:00 → 10:00 Asia/Ho_Chi_Minh", "BTCUSDT", "$108420.00", "+1.82% 🟢", "Vol: 148976.11428", "SOLUSDT", "-2.13% 🔴", "Vol: 52340.25"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("message missing %q: %s", expected, rendered)
		}
	}
}

func TestBuildMessageIncludesUnavailableSymbol(t *testing.T) {
	message, err := BuildMessage(testPeriod(),
		[]PriceResult{
			{
				Change:      domain.PriceChange{Symbol: "ETHUSDT"},
				Unavailable: true,
			},
		},
		testPeriod().Start.Location())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(RenderMessage(message), "ETHUSDT   unavailable ⚠️") {
		t.Fatal("expected unavailable symbol")
	}
}

func TestBuildMessageRejectsInvalidInput(t *testing.T) {
	if _, err := BuildMessage(testPeriod(), nil, time.UTC); err == nil {
		t.Fatal("expected empty results error")
	}
	if _, err := BuildMessage(testPeriod(), []PriceResult{{Change: domain.PriceChange{Symbol: "BTCUSDT"}}}, nil); err == nil {
		t.Fatal("expected location error")
	}
}
