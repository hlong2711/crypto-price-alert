package api

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func NewServer() *echo.Echo {
	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{LogStatus: true, LogURI: true, LogMethod: true}))
	handler := &Handler{}
	e.GET("/health", handler.Health)
	return e
}
