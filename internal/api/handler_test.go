package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/notification"
	"crypto-price-alert/internal/scheduler"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
)

func TestHealth(t *testing.T) {
	e := echo.New()
	record := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/health", nil)
	ctx := e.NewContext(request, record)
	if err := NewHandler(nil, nil).Health(ctx); err != nil {
		t.Fatal(err)
	}
	if record.Code != 200 {
		t.Fatalf("status=%d, want 200", record.Code)
	}
	if record.Body.String() != `{"status":"ok"}
` {
		t.Fatalf("body=%q", record.Body.String())
	}
}

func TestNewServerRegistersRoutes(t *testing.T) {
	e := NewServer(nil, nil)
	record := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/health", nil)
	e.ServeHTTP(record, request)
	if record.Code != 200 {
		t.Fatalf("status=%d, want 200", record.Code)
	}

	routes := make([]string, 0)
	for _, r := range e.Routes() {
		routes = append(routes, r.Method+" "+r.Path)
	}
	found := false
	for _, r := range routes {
		if r == "POST /api/v1/alerts/run" {
			found = true
		}
	}
	if !found {
		t.Fatalf("POST /api/v1/alerts/run not registered, routes=%v", routes)
	}
}

// --- fakes for RunAlert ---

type stubProvider struct {
	candle domain.Candle
	err    error
}

func (s stubProvider) GetKline(_ context.Context, symbol string, _ domain.Interval, start, end time.Time) (domain.Candle, error) {
	if s.err != nil {
		return domain.Candle{}, s.err
	}
	c := s.candle
	c.Symbol = symbol
	return c, nil
}

var _ market.MarketDataProvider = stubProvider{}

type stubNotifier struct {
	sent *int
	err  error
}

func (s stubNotifier) Send(_ context.Context, _ domain.Message) error {
	if s.sent != nil {
		*s.sent++
	}
	return s.err
}

var _ notification.Notifier = stubNotifier{}

func testDeps(t *testing.T, provider market.MarketDataProvider, sent *int) (*service.AlertService, *scheduler.PeriodEngine) {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	periods, err := scheduler.NewPeriodEngine(loc)
	if err != nil {
		t.Fatal(err)
	}
	alerts, err := service.NewAlertService(provider, []notification.Notifier{stubNotifier{sent: sent}}, []string{"test"}, []string{"BTCUSDT"}, loc)
	if err != nil {
		t.Fatal(err)
	}
	return alerts, periods
}

func doRunAlert(e *echo.Echo, body string) *httptest.ResponseRecorder {
	record := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/alerts/run", bytes.NewBufferString(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(record, request)
	return record
}

func TestRunAlertDryRun(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	start := time.Date(2026, 9, 4, 9, 0, 0, 0, loc)
	end := time.Date(2026, 9, 4, 10, 0, 0, 0, loc)
	candle := domain.Candle{Symbol: "BTCUSDT", Open: 100, High: 102, Low: 99, Close: 101,
		Volume: 1234, OpenTime: start.Add(time.Minute), CloseTime: end.Add(-time.Second)}
	var sent int
	alerts, periods := testDeps(t, stubProvider{candle: candle}, &sent)
	e := NewServer(alerts, periods)

	record := doRunAlert(e, `{"interval":"1h","symbols":["BTCUSDT"],"period_start":"`+start.Format(time.RFC3339)+`","period_end":"`+end.Format(time.RFC3339)+`","dry_run":true}`)
	if record.Code != 200 {
		t.Fatalf("status=%d body=%s, want 200", record.Code, record.Body.String())
	}
	if sent != 0 {
		t.Fatalf("dry_run sent %d notifications, want 0", sent)
	}
	if !strings.Contains(record.Body.String(), `"volume":1234`) {
		t.Fatalf("response missing volume: %s", record.Body.String())
	}
}

func TestRunAlertInvalidInterval(t *testing.T) {
	var sent int
	alerts, periods := testDeps(t, stubProvider{}, &sent)
	e := NewServer(alerts, periods)

	record := doRunAlert(e, `{"interval":"5m","dry_run":true}`)
	if record.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", record.Code, record.Body.String())
	}
}

func TestRunAlertSkippedOutsideWindow(t *testing.T) {
	var sent int
	alerts, periods := testDeps(t, stubProvider{}, &sent)
	e := NewServer(alerts, periods)

	// 03:00 local time is outside the 06:00-23:00 active window.
	record := doRunAlert(e, `{"interval":"1h","at":"2026-09-04T03:00:00+07:00","dry_run":true}`)
	if record.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", record.Code, record.Body.String())
	}
}
