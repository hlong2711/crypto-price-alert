package market

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func TestCoinMarketCapProviderPublicRequestAndUnixSeconds(t *testing.T) {
	var got *http.Request
	provider, err := NewCoinMarketCapProvider("http://cmc.test", "", "usd", map[string]CoinMarketCapSymbol{
		"BTC": {Platform: "ethereum", Address: "0xtoken"},
	}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		return response(http.StatusOK, `[["100","110","90","105","1234.5",1756681200,0]]`), nil
	})}, 1, time.Millisecond, 1)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1756681200, 0)
	candle, err := provider.GetKline(context.Background(), "BTC", domain.Interval1H, start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if candle.Symbol != "BTC" || candle.OpenTime != start.UTC() || candle.CloseTime != start.Add(time.Hour).UTC() {
		t.Fatalf("unexpected candle: %+v", candle)
	}
	if got.URL.Path != "/public-api/v1/k-line/candles" || got.Header.Get("X-CMC_PRO_API_KEY") != "" {
		t.Fatalf("unexpected public request: %s headers=%v", got.URL, got.Header)
	}
	query := got.URL.Query()
	if query.Get("platform") != "ethereum" || query.Get("address") != "0xtoken" || query.Get("interval") != "1h" || query.Get("from") != "1756681200" || query.Get("to") != "1756684800" || query.Get("limit") != "2" {
		t.Fatalf("unexpected query: %v", query)
	}
}

func TestCoinMarketCapProviderKeyedRequestAndStrictTimestamp(t *testing.T) {
	var got *http.Request
	provider, err := NewCoinMarketCapProvider("http://cmc.test", "secret", "usd", map[string]CoinMarketCapSymbol{
		"BTC": {Platform: "ethereum", Address: "0xtoken"},
	}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		return response(http.StatusOK, `[[100,110,90,105,1234.5,1756681200.5,0]]`), nil
	})}, 1, time.Millisecond, 1)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1756681200, 0)
	if _, err := provider.GetKline(context.Background(), "BTC", domain.Interval1H, start, start.Add(time.Hour)); err == nil {
		t.Fatal("expected fractional Unix timestamp to be rejected")
	}
	if got.URL.Path != "/v1/k-line/candles" || got.Header.Get("X-CMC_PRO_API_KEY") != "secret" {
		t.Fatalf("unexpected keyed request: %s headers=%v", got.URL, got.Header)
	}
}

func TestCoinMarketCapProviderRetriesServerError(t *testing.T) {
	calls := 0
	provider, err := NewCoinMarketCapProvider("http://cmc.test", "", "usd", map[string]CoinMarketCapSymbol{"BTC": {Platform: "eth", Address: "token"}}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return response(http.StatusBadGateway, "temporary"), nil
		}
		return response(http.StatusOK, `[[100,110,90,105,1,1756681200,0]]`), nil
	})}, 3, time.Millisecond, 1)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1756681200, 0)
	if _, err := provider.GetKline(context.Background(), "BTC", domain.Interval1H, start, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
}

func TestCoinMarketCapProviderDoesNotRetryClientError(t *testing.T) {
	calls := 0
	provider, err := NewCoinMarketCapProvider("http://cmc.test", "", "usd", map[string]CoinMarketCapSymbol{"BTC": {Platform: "eth", Address: "token"}}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("do not expose")), Header: make(http.Header)}, nil
	})}, 3, time.Millisecond, 1)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1756681200, 0)
	if _, err := provider.GetKline(context.Background(), "BTC", domain.Interval1H, start, start.Add(time.Hour)); err == nil || strings.Contains(err.Error(), "do not expose") {
		t.Fatal("expected status-only non-retryable error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestCoinMarketCapIntervalMapping(t *testing.T) {
	for interval, expected := range map[domain.Interval]string{domain.Interval1M: "1min", domain.Interval3M: "3min", domain.Interval5M: "5min", domain.Interval15M: "15min", domain.Interval30M: "30min", domain.Interval1H: "1h", domain.Interval1D: "1d", domain.Interval1W: "1w"} {
		actual, err := coinMarketCapInterval(interval)
		if err != nil || actual != expected {
			t.Fatalf("%s: got %q, err=%v; want %q", interval, actual, err, expected)
		}
	}
	if _, err := coinMarketCapInterval(domain.Interval("10m")); err == nil {
		t.Fatal("expected removed 10m interval to be unsupported")
	}
}
