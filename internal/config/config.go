package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App           AppConfig           `yaml:"app"`
	Database      DatabaseConfig      `yaml:"database"`
	Market        MarketConfig        `yaml:"market"`
	Schedule      ScheduleConfig      `yaml:"schedule"`
	Notifications NotificationsConfig `yaml:"notifications"`
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
	URL string `yaml:"url"`
}

type MarketConfig struct {
	Provider  string   `yaml:"provider"`
	Symbols   []string `yaml:"symbols"`
	Intervals []string `yaml:"intervals"`
}

type ScheduleConfig struct {
	ActiveFrom  string `yaml:"active_from"`
	ActiveUntil string `yaml:"active_until"`
}

type NotificationsConfig struct {
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
	if c.Market.Provider != "binance" {
		return fmt.Errorf("market.provider %q is unsupported", c.Market.Provider)
	}
	if len(c.Market.Symbols) == 0 {
		return errors.New("market.symbols must not be empty")
	}
	if len(c.Market.Intervals) == 0 {
		return errors.New("market.intervals must not be empty")
	}
	for _, interval := range c.Market.Intervals {
		if interval != "1h" && interval != "4h" {
			return fmt.Errorf("market.intervals: unsupported interval %q", interval)
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
	return nil
}
