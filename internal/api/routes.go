package api

import (
	"crypto-price-alert/internal/scheduler"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func NewServer(alerts *service.AlertService, periods *scheduler.PeriodEngine) *echo.Echo {
	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	e.Pre(middleware.RemoveTrailingSlash())

	handler := NewHandler(alerts, periods)
	e.GET("/api/health", handler.Health)
	e.POST("/api/v1/alerts/run", handler.RunAlert)
	return e
}

// RegisterTelegramWebhook attaches an authenticated Telegram adapter handler
// without making the API package depend on Telegram payload types.
func RegisterTelegramWebhook(e *echo.Echo, path string, handler echo.HandlerFunc) {
	if path == "" {
		path = "/api/v1/chat/telegram/webhook"
	}
	e.POST(path, handler)
}
