package main

import (
	"log/slog"
	"os"

	"crypto-price-alert/internal/config"
	"crypto-price-alert/internal/database"

	"crypto-price-alert/internal/api"
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

	dbUrl := cfg.Database.URL
	_, err = database.InitDatabase(dbUrl)
	if err == nil {
		logger.Info("init db done")
	}

	initServer(logger)
}

func initServer(logger *slog.Logger) {
	e := api.NewServer()

	if e != nil {
		logger.Info("server initialized successfully >>>")
	}

	e.Logger.Fatal(e.Start(":8080"))
}
