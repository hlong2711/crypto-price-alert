package market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto-price-alert/internal/domain"
)

const DefaultBinanceFuturesURL = "https://fapi.binance.com"
const maxFuturesBody = 8 << 20

type BinanceFuturesProvider struct {
	baseURL        string
	client         *http.Client
	attempts       int
	backoff        time.Duration
	mu             sync.Mutex
	cooldown       time.Time
	cooldownStatus int
	now            func() time.Time
	sleep          func(context.Context, time.Duration) error
}

func NewBinanceFuturesProvider(baseURL string, client *http.Client, attempts int, backoff time.Duration) (*BinanceFuturesProvider, error) {
	if baseURL == "" {
		baseURL = DefaultBinanceFuturesURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || attempts < 1 || backoff <= 0 {
		return nil, fmt.Errorf("invalid futures provider settings")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &BinanceFuturesProvider{
		baseURL:  strings.TrimRight(baseURL, "/"),
		client:   client,
		attempts: attempts,
		backoff:  backoff,
		now:      time.Now,
		sleep:    fundingSleep,
	}, nil
}

func fundingSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (p *BinanceFuturesProvider) request(ctx context.Context, path string) ([]byte, error) {
	for attempt := 0; attempt < p.attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		remaining := p.cooldown.Sub(p.now())
		status := p.cooldownStatus
		p.mu.Unlock()
		if remaining > 0 {
			return nil, &FundingUpstreamError{Status: status, RetryAfter: remaining}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
		if err != nil {
			return nil, invalidFundingPayload()
		}
		resp, err := p.client.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxFuturesBody+1))
			resp.Body.Close()
			if resp.StatusCode == 429 || resp.StatusCode == 418 {
				delay := time.Minute
				if resp.StatusCode == 418 {
					delay = 72 * time.Hour
				}
				if parsed, ok := fundingRetryAfter(resp.Header.Get("Retry-After"), p.now()); ok {
					delay = parsed
				}
				p.mu.Lock()
				until := p.now().Add(delay)
				if until.After(p.cooldown) {
					p.cooldown = until
					p.cooldownStatus = resp.StatusCode
				}
				p.mu.Unlock()
				return nil, &FundingUpstreamError{Status: resp.StatusCode, RetryAfter: delay}
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if e, ok := readErr.(interface{ Timeout() bool }); ok && e.Timeout() {
					return nil, context.DeadlineExceeded
				}
				if readErr != nil || len(body) > maxFuturesBody {
					return nil, invalidFundingPayload()
				}
				return body, nil
			}
			if resp.StatusCode < 500 {
				return nil, &FundingUpstreamError{Status: resp.StatusCode}
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// A client timeout is still a deadline failure even without a parent deadline.
		if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
			return nil, context.DeadlineExceeded
		}
		if attempt+1 == p.attempts {
			return nil, &FundingUpstreamError{Status: 502}
		}
		delay := p.backoff
		for i := 0; i < attempt && delay < 30*time.Second; i++ {
			delay *= 2
		}
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		if err := p.sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, invalidFundingPayload()
}

func fundingRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if until, err := http.ParseTime(value); err == nil && until.After(now) {
		return until.Sub(now), true
	}
	return 0, false
}

func decodeFundingArray(body []byte) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil || rows == nil {
		return nil, invalidFundingPayload()
	}
	return rows, nil
}

func (p *BinanceFuturesProvider) FundingContracts(ctx context.Context) ([]domain.FundingContract, error) {
	body, err := p.request(ctx, "/fapi/v1/exchangeInfo")
	if err != nil {
		return nil, err
	}
	var response struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
			Base   string `json:"baseAsset"`
			Quote  string `json:"quoteAsset"`
			Status string `json:"status"`
			Type   string `json:"contractType"`
		} `json:"symbols"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Symbols) == 0 {
		return nil, invalidFundingPayload()
	}
	out := make([]domain.FundingContract, 0)
	seen := map[string]bool{}
	for _, row := range response.Symbols {
		if row.Symbol == "" || row.Base == "" || row.Quote == "" || row.Status == "" || row.Type == "" || seen[row.Symbol] {
			return nil, invalidFundingPayload()
		}
		seen[row.Symbol] = true
		if row.Status != "TRADING" || row.Type != "PERPETUAL" || (row.Quote != "USDT" && row.Quote != "USDC") {
			continue
		}
		c := domain.FundingContract{Symbol: row.Symbol, BaseAsset: row.Base, QuoteAsset: row.Quote}
		if c.Validate() != nil {
			return nil, invalidFundingPayload()
		}
		out = append(out, c)
	}
	return out, nil
}

func (p *BinanceFuturesProvider) FundingIntervals(ctx context.Context) (map[string]int, error) {
	body, err := p.request(ctx, "/fapi/v1/fundingInfo")
	if err != nil {
		return nil, err
	}
	rows, err := decodeFundingArray(body)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, raw := range rows {
		var row struct {
			Symbol string `json:"symbol"`
			Hours  int    `json:"fundingIntervalHours"`
		}
		if json.Unmarshal(raw, &row) != nil || row.Symbol == "" || row.Hours <= 0 || row.Hours > 24 {
			return nil, invalidFundingPayload()
		}
		if _, exists := out[row.Symbol]; exists {
			return nil, invalidFundingPayload()
		}
		out[row.Symbol] = row.Hours
	}
	return out, nil
}

func (p *BinanceFuturesProvider) FundingObservations(ctx context.Context) ([]domain.FundingObservation, error) {
	body, err := p.request(ctx, "/fapi/v1/premiumIndex")
	if err != nil {
		return nil, err
	}
	rows, err := decodeFundingArray(body)
	if err != nil {
		return nil, err
	}
	out := make([]domain.FundingObservation, 0, len(rows))
	seen := map[string][]byte{}
	for _, raw := range rows {
		var row struct {
			Symbol string `json:"symbol"`
			Rate   string `json:"lastFundingRate"`
			Next   int64  `json:"nextFundingTime"`
			Time   int64  `json:"time"`
		}
		if json.Unmarshal(raw, &row) != nil {
			// Preserve the symbol when possible so service-level invalid counts
			// can distinguish corrupt eligible data from legitimate scarcity.
			var identity struct {
				Symbol string `json:"symbol"`
			}
			_ = json.Unmarshal(raw, &identity)
			out = append(out, domain.FundingObservation{Symbol: identity.Symbol})
			continue
		}
		if previous, exists := seen[row.Symbol]; exists {
			if !bytes.Equal(previous, raw) {
				return nil, invalidFundingPayload()
			}
			continue
		}
		seen[row.Symbol] = raw
		o := domain.FundingObservation{Symbol: row.Symbol, Rate: row.Rate}
		if row.Next > 0 {
			o.NextFundingTime = time.UnixMilli(row.Next).UTC()
		}
		if row.Time > 0 {
			o.ObservedAt = time.UnixMilli(row.Time).UTC()
		}
		out = append(out, o)
	}
	return out, nil
}

var _ FundingDataProvider = (*BinanceFuturesProvider)(nil)
