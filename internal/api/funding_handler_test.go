package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
)

type fundingReaderFunc func(context.Context, int) (domain.FundingRateResult, error)

func (f fundingReaderFunc) FundingRates(ctx context.Context, n int) (domain.FundingRateResult, error) {
	return f(ctx, n)
}

func TestFundingHTTPValidation(t *testing.T) {
	for _, query := range []string{"", "?limit=5", "?limit=20", "?limit=", "?limit=4", "?limit=21", "?limit=5.5", "?limit=5&limit=10", "?limit=%zz"} {
		t.Run(query, func(t *testing.T) {
			calls := 0
			e := echo.New()
			RegisterFundingRoutes(e, fundingReaderFunc(func(ctx context.Context, n int) (domain.FundingRateResult, error) {
				calls++
				return domain.FundingRateResult{RequestedLimit: n, Shortfall: true}, nil
			}))
			r := httptest.NewRecorder()
			e.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/funding-rates"+query, nil))
			valid := query == "" || query == "?limit=5" || query == "?limit=20"
			if valid {
				if r.Code != 200 || calls != 1 || !strings.Contains(r.Body.String(), `"data":[]`) {
					t.Fatal(r.Code, r.Body.String(), calls)
				}
			} else if r.Code != 400 || calls != 0 {
				t.Fatal(r.Code, r.Body.String(), calls)
			}
		})
	}
}

func TestFundingHTTPErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		retry  string
	}{
		{errors.New("secret upstream payload"), 502, ""}, {context.DeadlineExceeded, 504, ""},
		{service.ErrFundingUnavailable, 503, ""}, {&market.FundingUpstreamError{Status: 429, RetryAfter: 1500 * time.Millisecond}, 503, "2"},
		{&market.FundingUpstreamError{Status: 418, RetryAfter: time.Minute}, 503, "60"},
	} {
		e := echo.New()
		RegisterFundingRoutes(e, fundingReaderFunc(func(context.Context, int) (domain.FundingRateResult, error) {
			return domain.FundingRateResult{}, tc.err
		}))
		r := httptest.NewRecorder()
		e.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/funding-rates", nil))
		if r.Code != tc.status || r.Header().Get("Retry-After") != tc.retry || strings.Contains(r.Body.String(), "secret") {
			t.Fatal(r.Code, r.Body.String(), r.Header())
		}
	}
	e := echo.New()
	RegisterFundingRoutes(e, nil)
	r := httptest.NewRecorder()
	e.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/funding-rates", nil))
	if r.Code != 503 {
		t.Fatal(r.Code)
	}
}

func TestFundingHTTPSerializationAndScopedThrottle(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	e := NewServer(nil, nil)
	RegisterFundingRoutes(e, fundingReaderFunc(func(ctx context.Context, n int) (domain.FundingRateResult, error) {
		return domain.FundingRateResult{Source: "binance_usdm", RequestedLimit: n, ReturnedCount: 1, EligiblePositiveCount: 1, Shortfall: true, GeneratedAt: now, ObservedAt: &now, Data: []domain.FundingRate{{Symbol: "BTCUSDT", Rate: "0.00050000", IntervalHours: 8, ObservedAt: now, NextFundingTime: now.Add(time.Hour)}}}, nil
	}))
	r := httptest.NewRecorder()
	e.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/funding-rates", nil))
	var result domain.FundingRateResult
	if json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Data[0].Rate != "0.00050000" || !result.Shortfall || !strings.Contains(r.Body.String(), "Z\"") {
		t.Fatal(r.Body.String())
	}
	for range 20 {
		r = httptest.NewRecorder()
		e.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/funding-rates", nil))
	}
	if r.Code != 429 {
		t.Fatal("route not throttled", r.Code)
	}
	r = httptest.NewRecorder()
	e.ServeHTTP(r, httptest.NewRequest("GET", "/api/health", nil))
	if r.Code != 200 {
		t.Fatal("funding limiter affected health")
	}
}
