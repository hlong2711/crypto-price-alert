package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func futuresTestProvider(t *testing.T, handler http.HandlerFunc) *BinanceFuturesProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p, err := NewBinanceFuturesProvider(server.URL, server.Client(), 3, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	p.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return p
}

func TestFuturesTransportStatuses(t *testing.T) {
	for _, status := range []int{200, 400, 418, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
				io.WriteString(w, "[]")
			})
			_, err := p.request(context.Background(), "/test")
			want := 1
			if status == 500 {
				want = 3
			}
			if calls != want || (err == nil) != (status == 200) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if status == 418 || status == 429 {
				var upstream *FundingUpstreamError
				if !errors.As(err, &upstream) || upstream.RetryAfter != 120*time.Second {
					t.Fatal(err)
				}
				p.request(context.Background(), "/test")
				if calls != 1 {
					t.Fatal("ignored shared cooldown")
				}
			}
		})
	}
}

func TestFuturesTransportBoundsAndCancellation(t *testing.T) {
	p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", maxFuturesBody+1)) })
	if _, err := p.request(context.Background(), "/test"); err == nil {
		t.Fatal("accepted oversized body")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.request(ctx, "/test"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p2 := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p2.request(ctx, "/test"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

type futuresRoundTrip func(*http.Request) (*http.Response, error)

func (f futuresRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFuturesNetworkRetryAndFallbackCooldown(t *testing.T) {
	calls := 0
	p, _ := NewBinanceFuturesProvider("", &http.Client{Transport: futuresRoundTrip(func(r *http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("network failure") })}, 2, time.Millisecond)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := p.request(context.Background(), "/test"); err == nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	for _, status := range []int{418, 429} {
		p = futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		_, err := p.request(context.Background(), "/test")
		var e *FundingUpstreamError
		if !errors.As(err, &e) || e.RetryAfter <= 0 {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	if d, ok := fundingRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); !ok || d != time.Minute {
		t.Fatal(d, ok)
	}
}

func TestFuturesPublicFixtures(t *testing.T) {
	p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Error("bulk request must have no symbol")
		}
		file := map[string]string{"/fapi/v1/exchangeInfo": "funding_exchange.json", "/fapi/v1/fundingInfo": "funding_intervals.json", "/fapi/v1/premiumIndex": "funding_snapshot.json"}[r.URL.Path]
		b, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		w.Write(b)
	})
	ctx := context.Background()
	contracts, err := p.FundingContracts(ctx)
	if err != nil || len(contracts) != 4 || contracts[1].QuoteAsset != "USDC" || contracts[3].Symbol != "币安人生USDT" {
		t.Fatal(contracts, err)
	}
	intervals, err := p.FundingIntervals(ctx)
	if err != nil || intervals["ORDIUSDC"] != 4 {
		t.Fatal(intervals, err)
	}
	rows, err := p.FundingObservations(ctx)
	if err != nil || len(rows) != 3 || rows[0].NextFundingTime.UnixMilli() != 1791993600000 || rows[0].Rate != "0.00003362" || rows[0].ObservedAt.Location() != time.UTC {
		t.Fatal(rows, err)
	}
}

func TestFuturesMetadataValidation(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"symbols":[]}`, `{"symbols":[{"symbol":"BTCUSDT"}]}`, `{"symbols":[{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","status":"TRADING","contractType":"PERPETUAL"},{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","status":"TRADING","contractType":"PERPETUAL"}]}`} {
		p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, err := p.FundingContracts(context.Background()); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	for _, body := range []string{`null`, `{}`, `[{}]`, `[{"symbol":"X","fundingIntervalHours":0}]`, `[{"symbol":"X","fundingIntervalHours":-1}]`, `[{"symbol":"X","fundingIntervalHours":4},{"symbol":"X","fundingIntervalHours":8}]`} {
		p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, err := p.FundingIntervals(context.Background()); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `[]`) })
	if v, err := p.FundingIntervals(context.Background()); err != nil || len(v) != 0 {
		t.Fatal(v, err)
	}
	for _, body := range []string{`null`, `{}`, `[`, `[{"symbol":"X","lastFundingRate":"0.1"},{"symbol":"X","lastFundingRate":"0.2"}]`} {
		p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, err := p.FundingObservations(context.Background()); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestFuturesEligibilityAndMalformedRow(t *testing.T) {
	body := `{"symbols":[{"symbol":"AUSDT","baseAsset":"A","quoteAsset":"USDT","status":"SETTLING","contractType":"PERPETUAL"},{"symbol":"BUSDT","baseAsset":"B","quoteAsset":"USDT","status":"TRADING","contractType":"CURRENT_QUARTER"},{"symbol":"ETHBTC","baseAsset":"ETH","quoteAsset":"BTC","status":"TRADING","contractType":"PERPETUAL"}]}`
	p := futuresTestProvider(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
	if rows, err := p.FundingContracts(context.Background()); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	body = `[{"symbol":"BTCUSDT","lastFundingRate":123},null]`
	rows, err := p.FundingObservations(context.Background())
	if err != nil || len(rows) != 2 || rows[0].Symbol != "BTCUSDT" || rows[0].Rate != "" {
		t.Fatal(rows, err)
	}
}
