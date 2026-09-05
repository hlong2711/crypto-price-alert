package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"crypto-price-alert/internal/domain"
)

const defaultBinanceURL = "https://api.binance.com"

type BinanceProvider struct {
	baseURL     string
	client      *http.Client
	maxAttempts int
	backoff     time.Duration
	semaphore   chan struct{}
}

func NewBinanceProvider(baseURL string, client *http.Client, maxAttempts int, backoff time.Duration, concurrency int) (*BinanceProvider, error) {
	if baseURL == "" {
		baseURL = defaultBinanceURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if maxAttempts < 1 || backoff <= 0 || concurrency < 1 {
		return nil, fmt.Errorf("invalid Binance provider settings")
	}
	return &BinanceProvider{
		baseURL:     baseURL,
		client:      client,
		maxAttempts: maxAttempts,
		backoff:     backoff,
		semaphore:   make(chan struct{}, concurrency),
	}, nil
}

func (p *BinanceProvider) GetKline(ctx context.Context, symbol string, interval domain.Interval, start, end time.Time) (domain.Candle, error) {
	if symbol == "" || interval.Validate() != nil || start.IsZero() || end.IsZero() || !end.After(start) {
		return domain.Candle{}, fmt.Errorf("invalid kline request")
	}
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }() //limit concurrency
	case <-ctx.Done():
		return domain.Candle{}, ctx.Err()
	}

	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=1", p.baseURL, symbol, interval, start.UnixMilli(), end.UnixMilli())
	var lastErr error
	for attempt := 0; attempt < p.maxAttempts; attempt++ {
		candle, retry, err := p.request(ctx, url, symbol, start, end)
		if err == nil {
			return candle, nil
		}
		lastErr = err
		if !retry || attempt == p.maxAttempts-1 {
			break
		}
		timer := time.NewTimer(p.backoff * time.Duration(1<<attempt))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return domain.Candle{}, ctx.Err()
		}
	}
	return domain.Candle{}, lastErr
}

func (p *BinanceProvider) request(ctx context.Context, url, symbol string, start, end time.Time) (domain.Candle, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Candle{}, false, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return domain.Candle{}, true, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return domain.Candle{}, true, readErr
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return domain.Candle{}, true, fmt.Errorf("Binance HTTP status %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.Candle{}, false, fmt.Errorf("Binance HTTP status %d: %s", resp.StatusCode, string(body))
	}

	/* response format:
	[
		[
			1499040000000,
			"0.01634790",
			"0.80000000",
			"0.01575800",
			"0.01577100",
			"148976.11427815",
			1499644799999,
			"2434.19055334",
			308,
			"1756.87402397",
			"28.46694368",
			"0"
		]
	]
	*/
	var rows [][]json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		return domain.Candle{}, false, fmt.Errorf("decode Binance kline response: %w", err)
	}
	if len(rows) == 0 || len(rows[0]) < 7 {
		return domain.Candle{}, false, fmt.Errorf("Binance kline response is empty or malformed")
	}
	values := make([]string, 7)
	for i := range values {
		rawValue := rows[0][i]
		if len(rawValue) > 0 && rawValue[0] == '"' {
			if err := json.Unmarshal(rawValue, &values[i]); err != nil {
				return domain.Candle{}, false, fmt.Errorf("decode Binance kline value %d: %w", i, err)
			}
		} else {
			values[i] = string(rawValue)
		}
	}
	openTimeMS, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil {
		return domain.Candle{}, false, fmt.Errorf("parse kline open time: %w", err)
	}
	closeTimeMS, err := strconv.ParseInt(values[6], 10, 64)
	if err != nil {
		return domain.Candle{}, false, fmt.Errorf("parse kline close time: %w", err)
	}
	parsePrice := func(value, name string) (float64, error) {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, fmt.Errorf("parse kline %s: %w", name, err)
		}
		return parsed, nil
	}
	open, err := parsePrice(values[1], "open")
	if err != nil {
		return domain.Candle{}, false, err
	}
	high, err := parsePrice(values[2], "high")
	if err != nil {
		return domain.Candle{}, false, err
	}
	low, err := parsePrice(values[3], "low")
	if err != nil {
		return domain.Candle{}, false, err
	}
	close, err := parsePrice(values[4], "close")
	if err != nil {
		return domain.Candle{}, false, err
	}
	candle := domain.Candle{
		Symbol:    symbol,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		OpenTime:  time.UnixMilli(openTimeMS),
		CloseTime: time.UnixMilli(closeTimeMS),
	}

	if err := candle.Validate(); err != nil {
		return domain.Candle{}, false, err
	}
	if candle.OpenTime.Before(start) || candle.OpenTime.After(end) {
		return domain.Candle{}, false, fmt.Errorf("kline is outside requested period")
	}
	return candle, false, nil
}
