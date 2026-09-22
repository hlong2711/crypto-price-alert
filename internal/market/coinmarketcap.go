package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-price-alert/internal/domain"
)

const defaultCoinMarketCapURL = "https://pro-api.coinmarketcap.com"

type CoinMarketCapSymbol struct {
	Platform string
	Address  string
}

type CoinMarketCapProvider struct {
	baseURL     string
	apiKey      string
	unit        string
	symbols     map[string]CoinMarketCapSymbol
	client      *http.Client
	maxAttempts int
	backoff     time.Duration
	semaphore   chan struct{}
}

func NewCoinMarketCapProvider(baseURL, apiKey, unit string, symbols map[string]CoinMarketCapSymbol, client *http.Client, maxAttempts int, backoff time.Duration, concurrency int) (*CoinMarketCapProvider, error) {
	if baseURL == "" {
		baseURL = defaultCoinMarketCapURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if unit == "" {
		unit = "usd"
	}
	if strings.ToLower(unit) != "usd" || maxAttempts < 1 || backoff <= 0 || concurrency < 1 {
		return nil, fmt.Errorf("invalid CoinMarketCap provider settings")
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid CoinMarketCap base URL: %w", err)
	}
	mappings := make(map[string]CoinMarketCapSymbol, len(symbols))
	for symbol, mapping := range symbols {
		if strings.TrimSpace(symbol) == "" || strings.TrimSpace(mapping.Platform) == "" || strings.TrimSpace(mapping.Address) == "" {
			return nil, fmt.Errorf("invalid CoinMarketCap symbol mapping for %q", symbol)
		}
		mappings[symbol] = mapping
	}
	return &CoinMarketCapProvider{
		baseURL:     baseURL,
		apiKey:      apiKey,
		unit:        strings.ToLower(unit),
		symbols:     mappings,
		client:      client,
		maxAttempts: maxAttempts,
		backoff:     backoff,
		semaphore:   make(chan struct{}, concurrency),
	}, nil
}

func (p *CoinMarketCapProvider) GetKline(ctx context.Context, symbol string, interval domain.Interval, start, end time.Time) (domain.Candle, error) {
	if symbol == "" || interval.Validate() != nil || start.IsZero() || end.IsZero() || !end.After(start) {
		return domain.Candle{}, fmt.Errorf("invalid kline request")
	}
	mapping, ok := p.symbols[symbol]
	if !ok {
		return domain.Candle{}, fmt.Errorf("CoinMarketCap symbol %q is not configured", symbol)
	}
	cmcInterval, err := coinMarketCapInterval(interval)
	if err != nil {
		return domain.Candle{}, err
	}
	requestURL, err := p.requestURL(mapping, cmcInterval, start, end)
	if err != nil {
		return domain.Candle{}, err
	}
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return domain.Candle{}, ctx.Err()
	}

	var lastErr error
	for attempt := 0; attempt < p.maxAttempts; attempt++ {
		candle, retry, requestErr := p.request(ctx, requestURL, symbol, interval, start, end)
		if requestErr == nil {
			return candle, nil
		}
		lastErr = requestErr
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

func (p *CoinMarketCapProvider) requestURL(mapping CoinMarketCapSymbol, interval string, start, end time.Time) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(p.baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("parse CoinMarketCap base URL: %w", err)
	}
	path := "/v1/k-line/candles"
	if p.apiKey == "" {
		path = "/public-api" + path
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	query := parsed.Query()
	query.Set("platform", mapping.Platform)
	query.Set("address", mapping.Address)
	query.Set("interval", interval)
	query.Set("from", strconv.FormatInt(start.UTC().Unix(), 10))
	query.Set("to", strconv.FormatInt(end.UTC().Unix(), 10))
	query.Set("unit", p.unit)
	query.Set("limit", "2")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (p *CoinMarketCapProvider) request(ctx context.Context, requestURL, symbol string, interval domain.Interval, start, end time.Time) (domain.Candle, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return domain.Candle{}, false, err
	}
	if p.apiKey != "" {
		req.Header.Set("X-CMC_PRO_API_KEY", p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return domain.Candle{}, true, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return domain.Candle{}, true, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return domain.Candle{}, true, fmt.Errorf("CoinMarketCap HTTP status %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.Candle{}, false, fmt.Errorf("CoinMarketCap HTTP status %d", resp.StatusCode)
	}

	var rows [][]json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		return domain.Candle{}, false, fmt.Errorf("decode CoinMarketCap kline response: %w", err)
	}
	periodStart := start.UTC()
	periodEnd := end.UTC()
	duration, _ := interval.Duration()
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		values := make([]float64, 5)
		for i := range values {
			value, parseErr := parseJSONFloat(row[i])
			if parseErr != nil {
				return domain.Candle{}, false, fmt.Errorf("parse CoinMarketCap kline value %d: %w", i, parseErr)
			}
			values[i] = value
		}
		seconds, parseErr := parseUnixSeconds(row[5])
		if parseErr != nil {
			return domain.Candle{}, false, fmt.Errorf("parse CoinMarketCap kline timestamp: %w", parseErr)
		}
		openTime := time.Unix(seconds, 0).UTC()
		if openTime != periodStart || openTime.Add(duration) != periodEnd {
			continue
		}
		candle := domain.Candle{
			Symbol: symbol, Open: values[0], High: values[1], Low: values[2],
			Close: values[3], Volume: values[4],
			OpenTime: openTime, CloseTime: openTime.Add(duration),
		}
		if err := candle.Validate(); err != nil {
			return domain.Candle{}, false, fmt.Errorf("validate CoinMarketCap candle: %w", err)
		}
		return candle, false, nil
	}
	return domain.Candle{}, false, fmt.Errorf("CoinMarketCap response has no candle for requested period")
}

func parseUnixSeconds(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
		value = strings.TrimSpace(text)
	}
	if value == "" {
		return 0, fmt.Errorf("empty timestamp")
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseJSONFloat(raw json.RawMessage) (float64, error) {
	var value float64
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
		return strconv.ParseFloat(text, 64)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, err
	}
	return value, nil
}

func coinMarketCapInterval(interval domain.Interval) (string, error) {
	intervals := map[domain.Interval]string{
		domain.Interval1M: "1min", domain.Interval3M: "3min", domain.Interval5M: "5min",
		domain.Interval15M: "15min", domain.Interval30M: "30min", domain.Interval1H: "1h",
		domain.Interval2H: "2h", domain.Interval4H: "4h", domain.Interval6H: "6h",
		domain.Interval8H: "8h", domain.Interval12H: "12h", domain.Interval1D: "1d",
		domain.Interval3D: "3d", domain.Interval1W: "1w",
	}
	value, ok := intervals[interval]
	if !ok {
		return "", fmt.Errorf("unsupported CoinMarketCap interval %q", interval)
	}
	return value, nil
}
