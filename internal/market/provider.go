package market

import (
	"context"
	"crypto-price-alert/internal/domain"
	"fmt"
	"time"
)

type MarketDataProvider interface {
	GetKline(ctx context.Context, symbol string, interval domain.Interval, start, end time.Time) (domain.Candle, error)
}

type ProviderResolver interface {
	Get(domain.MarketProvider) (MarketDataProvider, error)
	Supports(domain.MarketProvider, string, domain.Interval) bool
}

type StaticProviderResolver struct {
	provider MarketDataProvider
}

func NewStaticProviderResolver(provider MarketDataProvider) *StaticProviderResolver {
	return &StaticProviderResolver{provider: provider}
}

func (r *StaticProviderResolver) Get(provider domain.MarketProvider) (MarketDataProvider, error) {
	if r == nil || r.provider == nil {
		return nil, fmt.Errorf("market provider is unavailable")
	}
	if provider != domain.MarketProviderBinance {
		return nil, fmt.Errorf("market provider %q is not enabled", provider)
	}
	return r.provider, nil
}

func (r *StaticProviderResolver) Supports(provider domain.MarketProvider, _ string, _ domain.Interval) bool {
	return r != nil && r.provider != nil && provider == domain.MarketProviderBinance
}
