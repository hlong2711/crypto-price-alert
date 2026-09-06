package market

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testProvider(t *testing.T, transport roundTripFunc) *BinanceProvider {
	t.Helper()
	provider, err := NewBinanceProvider("http://binance.test", &http.Client{Transport: transport}, 3, time.Millisecond, 2)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

const klineJSON = `[["1756681200000","100","110","90","105","1234.5","1756684799999","0"]]`

const numericKlineJSON = `[[1756681200000,100,110,90,105,678.9,1756684799999,0]]`

func TestBinanceProviderGetKline(t *testing.T) {
	provider := testProvider(t, func(r *http.Request) (*http.Response, error) { return response(http.StatusOK, klineJSON), nil })
	start := time.UnixMilli(1756681200000)
	candle, err := provider.GetKline(context.Background(), "BTCUSDT", domain.Interval1H, start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if candle.Symbol != "BTCUSDT" || candle.Open != 100 || candle.Close != 105 || candle.Volume != 1234.5 {
		t.Fatalf("unexpected candle: %+v", candle)
	}
}

func TestBinanceProviderGetKlineWithNumericValues(t *testing.T) {
	provider := testProvider(t, func(r *http.Request) (*http.Response, error) { return response(http.StatusOK, numericKlineJSON), nil })
	start := time.UnixMilli(1756681200000)
	candle, err := provider.GetKline(context.Background(), "BTCUSDT", domain.Interval1H, start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if candle.Symbol != "BTCUSDT" || candle.Open != 100 || candle.Close != 105 || candle.Volume != 678.9 {
		t.Fatalf("unexpected candle: %+v", candle)
	}
}

func TestBinanceProviderRetriesServerError(t *testing.T) {
	var calls atomic.Int32
	provider := testProvider(t, func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) < 3 {
			return response(http.StatusBadGateway, "temporary"), nil
		}
		return response(http.StatusOK, klineJSON), nil
	})
	start := time.UnixMilli(1756681200000)
	if _, err := provider.GetKline(context.Background(), "BTCUSDT", domain.Interval1H, start, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d, want 3", calls.Load())
	}
}

func TestBinanceProviderDoesNotRetryClientError(t *testing.T) {
	var calls atomic.Int32
	provider := testProvider(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(http.StatusBadRequest, "bad request"), nil
	})
	start := time.UnixMilli(1756681200000)
	if _, err := provider.GetKline(context.Background(), "BTCUSDT", domain.Interval1H, start, start.Add(time.Hour)); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1", calls.Load())
	}
}
