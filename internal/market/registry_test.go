package market

import (
	"testing"
	"time"

	"crypto-price-alert/internal/config"
	"crypto-price-alert/internal/domain"
)

func TestProviderRegistryBuildsEnabledProvidersAndCapabilities(t *testing.T) {
	marketConfig := config.MarketConfig{
		DefaultProvider: domain.MarketProviderBinance,
		Symbols:         []string{"BTC"}, Intervals: []string{"1m", "1h"},
		Providers: map[domain.MarketProvider]config.ProviderConfig{
			domain.MarketProviderBinance:       {Enabled: true, Symbols: map[string]config.ProviderSymbolConfig{"BTC": {Symbol: "BTCUSDT"}}},
			domain.MarketProviderCoinMarketCap: {Enabled: true, Symbols: map[string]config.ProviderSymbolConfig{"BTC": {Platform: "eth", Address: "token"}}},
		},
	}
	registry, err := NewProviderRegistry(marketConfig, nil, 1, time.Millisecond, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Enabled()) != 2 || !registry.Supports(domain.MarketProviderBinance, "BTC", domain.Interval1H) || registry.Supports(domain.MarketProviderBinance, "ETH", domain.Interval1H) {
		t.Fatalf("unexpected registry capabilities: enabled=%v", registry.Enabled())
	}
	if _, _, err := registry.Default(); err != nil {
		t.Fatal(err)
	}
}

func TestProviderRegistryRejectsMissingDefault(t *testing.T) {
	_, err := NewProviderRegistry(config.MarketConfig{DefaultProvider: domain.MarketProviderCoinMarketCap}, nil, 1, time.Millisecond, 1)
	if err == nil {
		t.Fatal("expected missing default provider error")
	}
}
