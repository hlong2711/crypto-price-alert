package market

import (
	"context"
	"crypto-price-alert/internal/domain"
	"time"
)

type MarketDataProvider interface {
	GetKline(ctx context.Context, symbol string, interval domain.Interval, start, end time.Time) (domain.Candle, error)
}
