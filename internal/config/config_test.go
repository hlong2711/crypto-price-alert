package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func validConfig() Config {
	return Config{
		App:      AppConfig{Timezone: "Asia/Ho_Chi_Minh", HTTP: HTTPConfig{Address: ":8080"}},
		Database: DatabaseConfig{URL: "postgres://localhost/crypto_alert"},
		Market: MarketConfig{
			DefaultProvider: domain.MarketProviderBinance,
			Symbols:         []string{"BTC"}, Intervals: []string{"1h", "4h"},
			Providers: map[domain.MarketProvider]ProviderConfig{
				domain.MarketProviderBinance: {Enabled: true, Symbols: map[string]ProviderSymbolConfig{"BTC": {Symbol: "BTCUSDT"}}},
			},
		},
		Schedule:      ScheduleConfig{ActiveFrom: "06:00", ActiveUntil: "23:00"},
		Notifications: NotificationsConfig{Telegram: TelegramConfig{Enabled: true, BotToken: "token", ChatID: "chat"}},
		Retry:         RetryConfig{MaxAttempts: 3, InitialBackoff: 500 * time.Millisecond},
		Concurrency:   ConcurrencyConfig{MarketRequests: 5},
	}
}

func TestConfigValidate(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestConfigValidateNotificationJobsRetention(t *testing.T) {
	cfg := validConfig()
	cfg.Database.NotificationJobsRetention = 720 * time.Hour
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected positive retention to validate, got %v", err)
	}

	cfg.Database.NotificationJobsRetention = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected zero retention to disable cleanup, got %v", err)
	}

	cfg.Database.NotificationJobsRetention = -time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected negative retention to fail validation")
	}
}

func TestConfigValidateRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"timezone", func(c *Config) { c.App.Timezone = "invalid/timezone" }},
		{"invalid_interval", func(c *Config) { c.Market.Intervals = []string{"10m"} }},
		{"invalid_tick_interval", func(c *Config) { c.Schedule.TickInterval = "invalid" }},
		{"channel", func(c *Config) { c.Notifications.Telegram.Enabled = false }},
		{"retry", func(c *Config) { c.Retry.MaxAttempts = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestConfigValidateChatSettings(t *testing.T) {
	cfg := validConfig()
	cfg.Chat = ChatConfig{Enabled: true, MaxSymbolsPerTarget: 20, MaxTargets: 10, WebhookBaseURL: "https://alerts.example.com", Telegram: ChatTelegramConfig{Enabled: true, BotToken: "token", WebhookSecret: "secret"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid chat config, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing webhook URL", func(c *Config) { c.Chat.WebhookBaseURL = "" }},
		{"missing Telegram secret", func(c *Config) { c.Chat.Telegram.WebhookSecret = "" }},
		{"missing platform", func(c *Config) { c.Chat.Telegram.Enabled = false }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := cfg
			tt.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("expected chat validation error")
			}
		})
	}
}

func TestLoadExpandsEnvironmentVariables(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "postgres://example/crypto_alert")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := `app:
  timezone: Asia/Ho_Chi_Minh
  http:
    address: ":8080"
database:
  url: "${TEST_DATABASE_URL}"
  notification_jobs_retention: 720h
market:
  default_provider: binance
  symbols: [BTC]
  intervals: [1h]
  providers:
    binance:
      enabled: true
      symbols:
        BTC: {symbol: BTCUSDT}
schedule:
  active_from: "06:00"
  active_until: "23:00"
notifications:
  dry: true
  telegram:
    enabled: true
    bot_token: token
    chat_id: chat
retry:
  max_attempts: 3
  initial_backoff: 500ms
concurrency:
  market_requests: 5
`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Database.URL != "postgres://example/crypto_alert" {
		t.Fatalf("unexpected database URL: %q", cfg.Database.URL)
	}
	if cfg.Database.NotificationJobsRetention != 720*time.Hour {
		t.Fatalf("unexpected notification job retention: %v", cfg.Database.NotificationJobsRetention)
	}
	if !cfg.Notifications.Dry {
		t.Fatal("expected notifications.dry to be true")
	}
}
