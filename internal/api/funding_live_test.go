package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
)

// TestFundingLive is opt-in and reads public Binance data; it never sends chat messages.
func TestFundingLive(t *testing.T) {
	if os.Getenv("FUNDING_LIVE_TEST") != "1" {
		t.Skip("set FUNDING_LIVE_TEST=1 for public Binance smoke test")
	}
	p, err := market.NewBinanceFuturesProvider("", &http.Client{Timeout: 10 * time.Second}, 2, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	s, err := service.NewFundingService(p, service.DefaultFundingSettings(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := echo.New()
	RegisterFundingRoutes(e, s)
	r := httptest.NewRecorder()
	e.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/funding-rates", nil))
	if r.Code != 200 {
		t.Fatalf("live endpoint status=%d body=%s", r.Code, r.Body.String())
	}
	var result domain.FundingRateResult
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ReturnedCount != min(5, result.EligiblePositiveCount) || result.ReturnedCount != len(result.Data) {
		t.Fatal("inconsistent live counts", result)
	}
	for i, row := range result.Data {
		n, err := domain.ParseFundingDecimal(row.Rate)
		if err != nil || n.Sign() <= 0 || row.IntervalHours <= 0 || !row.NextFundingTime.After(time.Now()) {
			t.Fatal("invalid live row", row, err)
		}
		if i > 0 {
			previous, _ := domain.ParseFundingDecimal(result.Data[i-1].Rate)
			if previous.Cmp(n) < 0 {
				t.Fatal("not sorted")
			}
		}
		t.Logf("%s rate=%s interval=%dh next=%s", row.Symbol, row.Rate, row.IntervalHours, row.NextFundingTime.Format(time.RFC3339))
	}
	t.Logf("eligible_positive_count=%d returned=%d excluded_invalid_count=%d", result.EligiblePositiveCount, result.ReturnedCount, result.ExcludedInvalidCount)
}
