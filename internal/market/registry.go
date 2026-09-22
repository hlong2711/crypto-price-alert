package market

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"crypto-price-alert/internal/config"
	"crypto-price-alert/internal/domain"
)

type providerCapabilities struct {
	symbols   map[string]struct{}
	intervals map[domain.Interval]struct{}
}

type ProviderRegistry struct {
	providers       map[domain.MarketProvider]MarketDataProvider
	capabilities    map[domain.MarketProvider]providerCapabilities
	defaultProvider domain.MarketProvider
}

func NewProviderRegistry(marketConfig config.MarketConfig, client *http.Client, maxAttempts int, backoff time.Duration, concurrency int) (*ProviderRegistry, error) {
	registry := &ProviderRegistry{
		providers:       make(map[domain.MarketProvider]MarketDataProvider),
		capabilities:    make(map[domain.MarketProvider]providerCapabilities),
		defaultProvider: marketConfig.DefaultProvider,
	}
	for providerName, providerConfig := range marketConfig.Providers {
		if !providerConfig.Enabled {
			continue
		}
		capabilities := providerCapabilities{symbols: make(map[string]struct{}), intervals: make(map[domain.Interval]struct{})}
		for symbol := range providerConfig.Symbols {
			capabilities.symbols[symbol] = struct{}{}
		}
		for _, rawInterval := range marketConfig.Intervals {
			capabilities.intervals[domain.Interval(rawInterval)] = struct{}{}
		}

		var provider MarketDataProvider
		var err error
		switch providerName {
		case domain.MarketProviderBinance:
			mappings := make(map[string]string, len(providerConfig.Symbols))
			for symbol, mapping := range providerConfig.Symbols {
				mappings[symbol] = mapping.Symbol
			}
			provider, err = NewBinanceProvider(providerConfig.BaseURL, client, maxAttempts, backoff, concurrency, mappings)

		case domain.MarketProviderCoinMarketCap:
			mappings := make(map[string]CoinMarketCapSymbol, len(providerConfig.Symbols))
			for symbol, mapping := range providerConfig.Symbols {
				mappings[symbol] = CoinMarketCapSymbol{Platform: mapping.Platform, Address: mapping.Address}
			}
			provider, err = NewCoinMarketCapProvider(providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Unit, mappings, client, maxAttempts, backoff, concurrency)

		default:
			err = fmt.Errorf("unsupported market provider %q", providerName)
		}

		if err != nil {
			return nil, fmt.Errorf("initialize market provider %s: %w", providerName, err)
		}
		registry.providers[providerName] = provider
		registry.capabilities[providerName] = capabilities
	}
	if _, ok := registry.providers[registry.defaultProvider]; !ok {
		return nil, fmt.Errorf("default market provider %q is not enabled", registry.defaultProvider)
	}
	return registry, nil
}

func (r *ProviderRegistry) Get(provider domain.MarketProvider) (MarketDataProvider, error) {
	if r == nil {
		return nil, fmt.Errorf("market provider registry is nil")
	}
	value, ok := r.providers[provider]
	if !ok {
		return nil, fmt.Errorf("market provider %q is not enabled", provider)
	}
	return value, nil
}

func (r *ProviderRegistry) Default() (domain.MarketProvider, MarketDataProvider, error) {
	provider, err := r.Get(r.defaultProvider)
	return r.defaultProvider, provider, err
}

func (r *ProviderRegistry) Enabled() []domain.MarketProvider {
	if r == nil {
		return nil
	}
	providers := make([]domain.MarketProvider, 0, len(r.providers))
	for provider := range r.providers {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	return providers
}

func (r *ProviderRegistry) Supports(provider domain.MarketProvider, symbol string, interval domain.Interval) bool {
	if r == nil {
		return false
	}
	capabilities, ok := r.capabilities[provider]
	if !ok {
		return false
	}
	_, symbolOK := capabilities.symbols[symbol]
	_, intervalOK := capabilities.intervals[interval]
	return symbolOK && intervalOK
}
