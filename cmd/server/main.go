package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"crypto-price-alert/internal/api"
	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/chat/slack"
	"crypto-price-alert/internal/chat/telegram"
	"crypto-price-alert/internal/config"
	"crypto-price-alert/internal/database"
	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/notification"
	"crypto-price-alert/internal/repository"
	"crypto-price-alert/internal/scheduler"
	"crypto-price-alert/internal/service"
	"crypto-price-alert/internal/service/configuration"
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

	notifiers, err := newNotifiers(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize notification channels", "error", err)
		os.Exit(1)
	}
	targetNotifiers, err := newTargetNotifiers(cfg, logger, notifiers)
	if err != nil {
		logger.Error("failed to initialize target notification channels", "error", err)
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
	targetExecutor, err := scheduler.NewTargetExecutor(periods, provider, jobRepo, jobRepo, jobRepo, targetNotifiers, executor, location)
	if err != nil {
		logger.Error("failed to initialize target scheduler executor", "error", err)
		os.Exit(1)
	}

	jobScheduler, err := scheduler.NewScheduler(location, targetExecutor)
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

	initServer(logger, cfg, cfg.App.HTTP.Address, alertService, periods, jobRepo)
}

func newNotifiers(cfg config.Config, logger *slog.Logger) ([]notification.Notifier, error) {
	notifiers := make([]notification.Notifier, 0, 2)
	if cfg.Notifications.Telegram.Enabled {
		var notifier notification.Notifier
		if cfg.Notifications.Dry {
			notifier = notification.NewDryRunNotifier("telegram", logger)
		} else {
			telegram, err := notification.NewTelegramNotifier(
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
			notifier = telegram
		}
		notifiers = append(notifiers, notifier)
	}

	if cfg.Notifications.Slack.Enabled {
		var notifier notification.Notifier
		if cfg.Notifications.Dry {
			notifier = notification.NewDryRunNotifier("slack", logger)
		} else {
			slack, err := notification.NewSlackNotifier(
				cfg.Notifications.Slack.WebhookURL,
				nil,
				cfg.Retry.MaxAttempts,
				cfg.Retry.InitialBackoff,
			)
			if err != nil {
				return nil, fmt.Errorf("slack: %w", err)
			}
			notifier = slack
		}
		notifiers = append(notifiers, notifier)
	}
	if len(notifiers) == 0 {
		return nil, fmt.Errorf("no notification channels are enabled")
	}
	return notifiers, nil
}

func newTargetNotifiers(cfg config.Config, logger *slog.Logger, legacy []notification.Notifier) ([]notification.Notifier, error) {
	if !cfg.Chat.Enabled {
		return legacy, nil
	}
	notifiers := make([]notification.Notifier, 0, 2)
	if cfg.Notifications.Dry {
		return []notification.Notifier{notification.NewDryRunNotifier("target", logger)}, nil
	}
	if cfg.Chat.Telegram.Enabled {
		telegram, err := notification.NewTelegramNotifier(cfg.Chat.Telegram.BotToken, "target", "", nil, cfg.Retry.MaxAttempts, cfg.Retry.InitialBackoff)
		if err != nil {
			return nil, fmt.Errorf("target telegram: %w", err)
		}
		notifiers = append(notifiers, telegram)
	}

	if cfg.Chat.Slack.Enabled {
		slack, err := notification.NewSlackBotNotifier(cfg.Chat.Slack.BotToken, "", nil, cfg.Retry.MaxAttempts, cfg.Retry.InitialBackoff)
		if err != nil {
			return nil, fmt.Errorf("target slack: %w", err)
		}
		notifiers = append(notifiers, slack)
	}
	if len(notifiers) == 0 {
		return legacy, nil
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

func initServer(logger *slog.Logger, cfg config.Config, address string, alerts *service.AlertService, periods *scheduler.PeriodEngine, repo *repository.PostgresRepository) {
	e := api.NewServer(alerts, periods)

	if cfg.Chat.Enabled && cfg.Chat.Telegram.Enabled {
		chatHandler, err := newTelegramChatHandler(cfg, repo)
		if err != nil {
			logger.Error("failed to initialize Telegram chat handler", "error", err)
			os.Exit(1)
		}
		api.RegisterTelegramWebhook(e, "/api/v1/chat/telegram/webhook", chatHandler.Webhook)
		err = chatHandler.RegisterCommands(context.Background())
		// register command is not fatal error
		if err != nil {
			logger.Error("failed to register Telegram commands", "error", err)
		} else {
			logger.Info("Telegram chat webhook registered", "path", "/api/v1/chat/telegram/webhook")
		}
	}
	if cfg.Chat.Enabled && cfg.Chat.Slack.Enabled {
		chatHandler, err := newSlackChatHandler(cfg, repo)
		if err != nil {
			logger.Error("failed to initialize Slack chat handler", "error", err)
			os.Exit(1)
		}
		api.RegisterSlackWebhook(e, "/api/v1/chat/slack/command", chatHandler.SlashCommandWebhook)
		api.RegisterSlackWebhook(e, "/api/v1/chat/slack/interaction", chatHandler.InteractionWebhook)
		logger.Info("Slack chat webhooks registered", "command_path", "/api/v1/chat/slack/command", "interaction_path", "/api/v1/chat/slack/interaction")
	}

	logger.Info("server initialized successfully", "address", address)

	if err := e.Start(address); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func newTelegramChatHandler(cfg config.Config, repo *repository.PostgresRepository) (*telegram.Adapter, error) {
	allowedIntervals := make([]domain.Interval, 0, len(cfg.Market.Intervals))
	for _, value := range cfg.Market.Intervals {
		allowedIntervals = append(allowedIntervals, domain.Interval(value))
	}
	configurationService, err := configuration.NewService(repo, repo, cfg.Market.Symbols, allowedIntervals, cfg.Chat.MaxSymbolsPerTarget, cfg.Chat.MaxTargets)
	if err != nil {
		return nil, err
	}
	sessions := chat.NewMemorySessionStore()
	chatService, err := chat.NewService(configurationService, chat.CreatorAuthorizer{}, sessions, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	client, err := telegram.NewAPIClient(cfg.Chat.Telegram.BotToken, "", nil)
	if err != nil {
		return nil, err
	}
	return telegram.NewAdapter(client, repo, repo, chatService, sessions, cfg.Chat.Telegram.WebhookSecret, "default", cfg.Market.Symbols, allowedIntervals)
}

func newSlackChatHandler(cfg config.Config, repo *repository.PostgresRepository) (*slack.Adapter, error) {
	allowedIntervals := make([]domain.Interval, 0, len(cfg.Market.Intervals))
	for _, value := range cfg.Market.Intervals {
		allowedIntervals = append(allowedIntervals, domain.Interval(value))
	}
	configurationService, err := configuration.NewService(repo, repo, cfg.Market.Symbols, allowedIntervals, cfg.Chat.MaxSymbolsPerTarget, cfg.Chat.MaxTargets)
	if err != nil {
		return nil, err
	}
	sessions := chat.NewMemorySessionStore()
	chatService, err := chat.NewService(configurationService, chat.CreatorAuthorizer{}, sessions, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	client, err := slack.NewAPIClient(cfg.Chat.Slack.BotToken, "", nil)
	if err != nil {
		return nil, err
	}
	return slack.NewAdapter(client, repo, repo, chatService, sessions, cfg.Chat.Slack.SigningSecret, "default", cfg.Chat.Slack.AppID, cfg.Chat.Slack.TeamID, cfg.Market.Symbols, allowedIntervals)
}
