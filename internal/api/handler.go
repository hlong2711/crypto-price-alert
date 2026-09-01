package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Handler struct{}

func (h *Handler) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
