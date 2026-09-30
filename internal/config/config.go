package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"crypto-price-alert/internal/domain"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App           AppConfig           `yaml:"app"`
	Database      DatabaseConfig      `yaml:"database"`
	Market        MarketConfig        `yaml:"market"`
	Schedule      ScheduleConfig      `yaml:"schedule"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Chat          ChatConfig          `yaml:"chat"`
	Retry         RetryConfig         `yaml:"retry"`
	Concurrency   ConcurrencyConfig   `yaml:"concurrency"`
}

type AppConfig struct {
	Timezone string     `yaml:"timezone"`
	HTTP     HTTPConfig `yaml:"http"`
}

type HTTPConfig struct {
	Address string `yaml:"address"`
}

type DatabaseConfig struct {
	URL                       string        `yaml:"url"`
	NotificationJobsRetention time.Duration `yaml:"notification_jobs_retention"`
}

type MarketConfig struct {
	DefaultProvider domain.MarketProvider                    `yaml:"default_provider"`
	Symbols         []string                                 `yaml:"symbols"`
	Intervals       []string                                 `yaml:"intervals"`
	Providers       map[domain.MarketProvider]ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Enabled bool                            `yaml:"enabled"`
	BaseURL string                          `yaml:"base_url"`
	APIKey  string                          `yaml:"api_key"`
	Unit    string                          `yaml:"unit"`
	Symbols map[string]ProviderSymbolConfig `yaml:"symbols"`
}

type ProviderSymbolConfig struct {
	Symbol   string `yaml:"symbol"`
	Platform string `yaml:"platform"`
	Address  string `yaml:"address"`
}

type ScheduleConfig struct {
	ActiveFrom   string `yaml:"active_from"`
	ActiveUntil  string `yaml:"active_until"`
	TickInterval string `yaml:"tick_interval"`
}

type NotificationsConfig struct {
	Dry      bool           `yaml:"dry"`
	Telegram TelegramConfig `yaml:"telegram"`
	Slack    SlackConfig    `yaml:"slack"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
}

type SlackConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
}

type ChatConfig struct {
	Enabled             bool               `yaml:"enabled"`
	MaxSymbolsPerTarget int                `yaml:"max_symbols_per_target"`
	MaxTargets          int                `yaml:"max_targets"`
	WebhookBaseURL      string             `yaml:"webhook_base_url"`
	Telegram            ChatTelegramConfig `yaml:"telegram"`
	Slack               ChatSlackConfig    `yaml:"slack"`
}

type ChatTelegramConfig struct {
	Enabled                 bool   `yaml:"enabled"`
	BotToken                string `yaml:"bot_token"`
	WebhookSecret           string `yaml:"webhook_secret"`
	SkipWebhookRegistration bool   `yaml:"skip_webhook_registration"`
}

type ChatSlackConfig struct {
	Enabled       bool   `yaml:"enabled"`
	SigningSecret string `yaml:"signing_secret"`
	BotToken      string `yaml:"bot_token"`
	AppID         string `yaml:"app_id"`
	TeamID        string `yaml:"team_id"`
}

type RetryConfig struct {
	MaxAttempts    int           `yaml:"max_attempts"`
	InitialBackoff time.Duration `yaml:"initial_backoff"`
}

type ConcurrencyConfig struct {
	MarketRequests int `yaml:"market_requests"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Schedule.TickInterval == "" {
		cfg.Schedule.TickInterval = "1m"
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.App.Timezone == "" {
		return errors.New("app.timezone is required")
	}
	if _, err := time.LoadLocation(c.App.Timezone); err != nil {
		return fmt.Errorf("app.timezone: %w", err)
	}
	if c.App.HTTP.Address == "" {
		return errors.New("app.http.address is required")
	}
	if strings.TrimSpace(c.Database.URL) == "" {
		return errors.New("database.url is required")
	}
	if c.Database.NotificationJobsRetention < 0 {
		return errors.New("database.notification_jobs_retention must not be negative")
	}
	if len(c.Market.Symbols) == 0 {
		return errors.New("market.symbols must not be empty")
	}
	if len(c.Market.Intervals) == 0 {
		return errors.New("market.intervals must not be empty")
	}
	seenSymbols := make(map[string]struct{}, len(c.Market.Symbols))
	for _, symbol := range c.Market.Symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			return errors.New("market.symbols contains an empty symbol")
		}
		if _, ok := seenSymbols[symbol]; ok {
			return fmt.Errorf("market.symbols contains duplicate %q", symbol)
		}
		seenSymbols[symbol] = struct{}{}
	}
	seenIntervals := make(map[domain.Interval]struct{}, len(c.Market.Intervals))
	for _, rawInterval := range c.Market.Intervals {
		interval := domain.Interval(strings.ToLower(strings.TrimSpace(rawInterval)))
		if err := domain.Interval(interval).Validate(); err != nil {
			return fmt.Errorf("market.intervals: %w", err)
		}
		if _, ok := seenIntervals[interval]; ok {
			return fmt.Errorf("market.intervals contains duplicate %q", interval)
		}
		seenIntervals[interval] = struct{}{}
	}
	if len(c.Market.Providers) == 0 {
		return errors.New("market.providers must not be empty")
	}
	defaultProvider := domain.MarketProvider(strings.ToLower(strings.TrimSpace(string(c.Market.DefaultProvider))))
	if err := defaultProvider.Validate(); err != nil {
		return fmt.Errorf("market.default_provider: %w", err)
	}
	defaultConfig, ok := c.Market.Providers[defaultProvider]
	if !ok || !defaultConfig.Enabled {
		return fmt.Errorf("market.default_provider %q must name an enabled provider", defaultProvider)
	}
	for provider, providerConfig := range c.Market.Providers {
		if err := provider.Validate(); err != nil {
			return err
		}
		if !providerConfig.Enabled {
			continue
		}
		if providerConfig.Unit == "" && provider == domain.MarketProviderCoinMarketCap {
			providerConfig.Unit = "usd"
		}
		if provider == domain.MarketProviderCoinMarketCap && strings.ToLower(providerConfig.Unit) != "usd" {
			return fmt.Errorf("market.providers.%s.unit %q is unsupported", provider, providerConfig.Unit)
		}
		for rawSymbol, mapping := range providerConfig.Symbols {
			symbol := strings.ToUpper(strings.TrimSpace(rawSymbol))
			if _, ok := seenSymbols[symbol]; !ok {
				return fmt.Errorf("market.providers.%s.symbols.%s is not in market.symbols", provider, rawSymbol)
			}
			switch provider {
			case domain.MarketProviderBinance:
				if strings.TrimSpace(mapping.Symbol) == "" {
					return fmt.Errorf("market.providers.%s.symbols.%s.symbol is required", provider, rawSymbol)
				}
			case domain.MarketProviderCoinMarketCap:
				if strings.TrimSpace(mapping.Platform) == "" || strings.TrimSpace(mapping.Address) == "" {
					return fmt.Errorf("market.providers.%s.symbols.%s requires platform and address", provider, rawSymbol)
				}
			}
		}
	}
	if c.Schedule.TickInterval != "" {
		if err := domain.Interval(strings.ToLower(strings.TrimSpace(c.Schedule.TickInterval))).Validate(); err != nil {
			return fmt.Errorf("schedule.tick_interval: %w", err)
		}
	}
	if _, err := time.Parse("15:04", c.Schedule.ActiveFrom); err != nil {
		return fmt.Errorf("schedule.active_from: %w", err)
	}
	if _, err := time.Parse("15:04", c.Schedule.ActiveUntil); err != nil {
		return fmt.Errorf("schedule.active_until: %w", err)
	}
	if c.Notifications.Telegram.Enabled && (c.Notifications.Telegram.BotToken == "" || c.Notifications.Telegram.ChatID == "") {
		return errors.New("enabled telegram requires bot_token and chat_id")
	}
	if c.Notifications.Slack.Enabled && c.Notifications.Slack.WebhookURL == "" {
		return errors.New("enabled slack requires webhook_url")
	}
	if !c.Notifications.Telegram.Enabled && !c.Notifications.Slack.Enabled {
		return errors.New("at least one notification channel must be enabled")
	}
	if c.Retry.MaxAttempts < 1 {
		return errors.New("retry.max_attempts must be at least 1")
	}
	if c.Retry.InitialBackoff <= 0 {
		return errors.New("retry.initial_backoff must be positive")
	}
	if c.Concurrency.MarketRequests < 1 {
		return errors.New("concurrency.market_requests must be at least 1")
	}
	if c.Chat.Enabled {
		if c.Chat.MaxSymbolsPerTarget < 1 {
			return errors.New("chat.max_symbols_per_target must be at least 1")
		}
		if c.Chat.MaxTargets < 1 {
			return errors.New("chat.max_targets must be at least 1")
		}
		if strings.TrimSpace(c.Chat.WebhookBaseURL) == "" {
			return errors.New("chat.webhook_base_url is required when chat is enabled")
		}
		if c.Chat.Telegram.Enabled && (strings.TrimSpace(c.Chat.Telegram.BotToken) == "" || strings.TrimSpace(c.Chat.Telegram.WebhookSecret) == "") {
			return errors.New("enabled chat.telegram requires bot_token and webhook_secret")
		}
		if c.Chat.Slack.Enabled && (strings.TrimSpace(c.Chat.Slack.SigningSecret) == "" || strings.TrimSpace(c.Chat.Slack.BotToken) == "") {
			return errors.New("enabled chat.slack requires signing_secret and bot_token")
		}
		if !c.Chat.Telegram.Enabled && !c.Chat.Slack.Enabled {
			return errors.New("at least one chat platform must be enabled")
		}
	}
	return nil
}
