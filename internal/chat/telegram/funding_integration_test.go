package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crypto-price-alert/internal/api"
	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
)

type telegramFundingProvider struct {
	now   time.Time
	err   error
	calls atomic.Int32
}

func (p *telegramFundingProvider) FundingContracts(context.Context) ([]domain.FundingContract, error) {
	p.calls.Add(1)
	out := []domain.FundingContract{}
	for i := 0; i < 10; i++ {
		out = append(out, domain.FundingContract{Symbol: fmt.Sprintf("C%02dUSDC", i), BaseAsset: fmt.Sprintf("C%02d", i), QuoteAsset: "USDC"})
	}
	return out, p.err
}
func (p *telegramFundingProvider) FundingIntervals(context.Context) (map[string]int, error) {
	p.calls.Add(1)
	return map[string]int{"C09USDC": 4}, p.err
}
func (p *telegramFundingProvider) FundingObservations(context.Context) ([]domain.FundingObservation, error) {
	p.calls.Add(1)
	out := []domain.FundingObservation{}
	for i := 0; i < 10; i++ {
		out = append(out, domain.FundingObservation{Symbol: fmt.Sprintf("C%02dUSDC", i), Rate: fmt.Sprintf("0.%03d", i+1), ObservedAt: p.now, NextFundingTime: p.now.Add(time.Hour)})
	}
	return out, p.err
}

// Any configuration method invocation would panic: funding must not need it.
type fundingUnusedConfiguration struct{ chat.ConfigurationService }
type countedFundingReader struct {
	service *service.FundingService
	calls   atomic.Int32
}

func (r *countedFundingReader) FundingRates(ctx context.Context, n int) (domain.FundingRateResult, error) {
	r.calls.Add(1)
	return r.service.FundingRates(ctx, n)
}

func fundingAdapter(t *testing.T, upstreamErr error) (*Adapter, *countedFundingReader, *telegramFundingProvider, *fakeEvents, *[]telegramAPICall) {
	t.Helper()
	provider := &telegramFundingProvider{now: time.Now().UTC(), err: upstreamErr}
	funding, err := service.NewFundingService(provider, service.DefaultFundingSettings(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(funding.Close)
	reader := &countedFundingReader{service: funding}
	sessions := chat.NewMemorySessionStore()
	commands, err := chat.NewService(&fundingUnusedConfiguration{}, chat.CreatorAuthorizer{}, sessions, time.Minute, chat.WithFundingReader(reader))
	if err != nil {
		t.Fatal(err)
	}
	calls := &[]telegramAPICall{}
	client, err := NewAPIClient("token", "http://telegram.test", newTelegramAPIRecorder(t, calls, 99))
	if err != nil {
		t.Fatal(err)
	}
	events := &fakeEvents{}
	adapter, err := NewAdapter(client, &fakeTargets{}, events, commands, sessions, "secret", "bot", []string{"BTCUSDT"}, []domain.Interval{domain.Interval1H}, market.NewStaticProviderResolver(&testMarketProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	return adapter, reader, provider, events, calls
}

func TestFundingWebhookAndAPIUseSameSnapshot(t *testing.T) {
	adapter, reader, provider, events, calls := fundingAdapter(t, nil)
	update := telegramMessage(500, "/funding") // actor 7 is not creator 99
	if code := postTelegramUpdate(t, adapter, update); code != 200 {
		t.Fatal(code)
	}
	text := lastPayloadForMethod(t, *calls, "sendMessage")["text"].(string)
	if !strings.Contains(text, "1. C09USDC | +1%/4h") || !strings.Contains(text, "5. C05USDC") || strings.Contains(text, "6. C04USDC") || !events.processed || events.failed {
		t.Fatal(text, events)
	}
	postTelegramUpdate(t, adapter, update)
	sends := 0
	for _, c := range *calls {
		if c.Method == "sendMessage" {
			sends++
		}
	}
	if sends != 1 || reader.calls.Load() != 1 || provider.calls.Load() != 3 {
		t.Fatal(sends, reader.calls.Load(), provider.calls.Load())
	}
	e := echo.New()
	api.RegisterFundingRoutes(e, reader)
	r := httptest.NewRecorder()
	e.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/funding-rates", nil))
	var result domain.FundingRateResult
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.Data) != 5 {
		t.Fatal(r.Code, r.Body.String())
	}
	for i, row := range result.Data {
		pct, _ := domain.FundingPercent(row.Rate)
		if !strings.Contains(text, fmt.Sprintf("%d. %s | %s", i+1, row.Symbol, pct)) {
			t.Fatal("API and Telegram ranking differ", text, result)
		}
	}
	if provider.calls.Load() != 3 {
		t.Fatal("API did not share Telegram cache")
	}
}

func TestFundingWebhookVariants(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		upstream         error
		executions       int32
	}{
		{"suffix", "/funding@MyBot 10", "10. C00USDC", nil, 1},
		{"invalid", "/funding 4", "Usage: /funding", nil, 0},
		{"unavailable", "/funding", "temporarily unavailable", errors.New("private upstream details"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, reader, _, events, calls := fundingAdapter(t, tc.upstream)
			if postTelegramUpdate(t, adapter, telegramMessage(501, tc.text)) != 200 {
				t.Fatal("webhook rejected")
			}
			text := lastPayloadForMethod(t, *calls, "sendMessage")["text"].(string)
			if !strings.Contains(text, tc.want) || strings.Contains(text, "private upstream") || reader.calls.Load() != tc.executions || !events.processed || events.failed {
				t.Fatal(text, events, reader.calls.Load())
			}
		})
	}
}

func TestFundingWebhookRejectsInvalidSecretBeforeExecution(t *testing.T) {
	adapter, reader, provider, events, calls := fundingAdapter(t, nil)
	update, _ := json.Marshal(telegramMessage(502, "/funding"))
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(update)))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong")
	r := httptest.NewRecorder()
	if err := adapter.Webhook(e.NewContext(req, r)); err != nil {
		t.Fatal(err)
	}
	if r.Code != 401 || events.claimed || reader.calls.Load() != 0 || provider.calls.Load() != 0 || len(*calls) != 0 {
		t.Fatal(r.Code, events, reader.calls.Load())
	}
}
