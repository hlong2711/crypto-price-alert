package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		App:           AppConfig{Timezone: "Asia/Ho_Chi_Minh", HTTP: HTTPConfig{Address: ":8080"}},
		Database:      DatabaseConfig{URL: "postgres://localhost/crypto_alert"},
		Market:        MarketConfig{Provider: "binance", Symbols: []string{"BTCUSDT"}, Intervals: []string{"1h", "4h"}},
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

func TestConfigValidateRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"timezone", func(c *Config) { c.App.Timezone = "invalid/timezone" }},
		{"interval", func(c *Config) { c.Market.Intervals = []string{"15m"} }},
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
market:
  provider: binance
  symbols: [BTCUSDT]
  intervals: [1h]
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
	if !cfg.Notifications.Dry {
		t.Fatal("expected notifications.dry to be true")
	}
}
