package main

import (
	"log/slog"
	"os"

	"crypto-price-alert/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	configPath := os.Getenv("CONFIG_FILE")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	if _, err := config.Load(configPath); err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
}
