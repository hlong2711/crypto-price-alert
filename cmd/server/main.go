package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"crypto-price-alert/internal/api"
	"crypto-price-alert/internal/config"
	"crypto-price-alert/internal/database"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/notification"
	"crypto-price-alert/internal/repository"
	"crypto-price-alert/internal/scheduler"
	"crypto-price-alert/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	configPath := os.Getenv("CONFIG_FILE")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	logger.Info("load config done")

	db, err := database.InitDatabase(cfg.Database.URL)
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	logger.Info("init db done")
	defer database.Close(db)

	location, err := time.LoadLocation(cfg.App.Timezone)
	if err != nil {
		logger.Error("failed to load timezone", "error", err)
		os.Exit(1)
	}

	jobRepo, err := repository.NewPostgresRepository(db)
	if err != nil {
		logger.Error("failed to initialize job repository", "error", err)
		os.Exit(1)
	}

	provider, err := market.NewBinanceProvider(
		"",
		&http.Client{Timeout: 10 * time.Second},
		cfg.Retry.MaxAttempts,
		cfg.Retry.InitialBackoff,
		cfg.Concurrency.MarketRequests,
	)
	if err != nil {
		logger.Error("failed to initialize market provider", "error", err)
		os.Exit(1)
	}

	notifiers, err := newNotifiers(cfg)
	if err != nil {
		logger.Error("failed to initialize notification channels", "error", err)
		os.Exit(1)
	}

	periods, err := scheduler.NewPeriodEngine(location)
	if err != nil {
		logger.Error("failed to initialize period engine", "error", err)
		os.Exit(1)
	}
	executor, err := scheduler.NewExecutor(periods, provider, jobRepo, notifiers, cfg.Market.Symbols, location)
	if err != nil {
		logger.Error("failed to initialize scheduler executor", "error", err)
		os.Exit(1)
	}
	jobScheduler, err := scheduler.NewScheduler(location, executor)
	if err != nil {
		logger.Error("failed to initialize scheduler", "error", err)
		os.Exit(1)
	}
	jobScheduler.Start()
	defer func() {
		if err := jobScheduler.Stop().Err(); err != nil {
			logger.Error("scheduler shutdown failed", "error", err)
		}
	}()
	logger.Info("scheduler started", "timezone", cfg.App.Timezone)

	alertService, err := service.NewAlertService(provider, notifiers, notifierNames(cfg), cfg.Market.Symbols, location)
	if err != nil {
		logger.Error("failed to initialize alert service", "error", err)
		os.Exit(1)
	}

	initServer(logger, cfg.App.HTTP.Address, alertService, periods)
}

func newNotifiers(cfg config.Config) ([]notification.Notifier, error) {
	notifiers := make([]notification.Notifier, 0, 2)
	if cfg.Notifications.Telegram.Enabled {
		notifier, err := notification.NewTelegramNotifier(
			cfg.Notifications.Telegram.BotToken,
			cfg.Notifications.Telegram.ChatID,
			"",
			nil,
			cfg.Retry.MaxAttempts,
			cfg.Retry.InitialBackoff,
		)
		if err != nil {
			return nil, fmt.Errorf("telegram: %w", err)
		}
		notifiers = append(notifiers, notifier)
	}

	if cfg.Notifications.Slack.Enabled {
		notifier, err := notification.NewSlackNotifier(
			cfg.Notifications.Slack.WebhookURL,
			nil,
			cfg.Retry.MaxAttempts,
			cfg.Retry.InitialBackoff,
		)
		if err != nil {
			return nil, fmt.Errorf("slack: %w", err)
		}
		notifiers = append(notifiers, notifier)
	}
	if len(notifiers) == 0 {
		return nil, fmt.Errorf("no notification channels are enabled")
	}
	return notifiers, nil
}

/* Get name list of enabled notifiers */
func notifierNames(cfg config.Config) []string {
	names := make([]string, 0, 2)
	if cfg.Notifications.Telegram.Enabled {
		names = append(names, "telegram")
	}
	if cfg.Notifications.Slack.Enabled {
		names = append(names, "slack")
	}
	return names
}

func initServer(logger *slog.Logger, address string, alerts *service.AlertService, periods *scheduler.PeriodEngine) {
	e := api.NewServer(alerts, periods)

	logger.Info("server initialized successfully", "address", address)

	if err := e.Start(address); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
