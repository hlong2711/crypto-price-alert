package api

import (
	"net/http"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/scheduler"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	alerts  *service.AlertService
	periods *scheduler.PeriodEngine
}

func NewHandler(alerts *service.AlertService, periods *scheduler.PeriodEngine) *Handler {
	return &Handler{alerts: alerts, periods: periods}
}

func (h *Handler) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

type runAlertRequest struct {
	Interval    string   `json:"interval"`
	Symbols     []string `json:"symbols"`
	At          string   `json:"at"`
	PeriodStart string   `json:"period_start"`
	PeriodEnd   string   `json:"period_end"`
	DryRun      *bool    `json:"dry_run"`
}

// RunAlert executes the job logic directly: fetch market data, build message, send notifications.
// It does not go through the cron scheduler and does not write to the job repository.
func (h *Handler) RunAlert(c echo.Context) error {
	if h.alerts == nil || h.periods == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "alert service not configured"})
	}

	var req runAlertRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	// Allow query params as an alternative for quick curl testing.
	if req.Interval == "" {
		req.Interval = c.QueryParam("interval")
	}
	if req.At == "" {
		req.At = c.QueryParam("at")
	}
	if req.DryRun == nil {
		if v := c.QueryParam("dry_run"); v != "" {
			dry := v == "true" || v == "1"
			req.DryRun = &dry
		}
	}

	interval := domain.Interval(req.Interval)
	if err := interval.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	symbols := req.Symbols
	if len(symbols) == 0 {
		symbols = h.alerts.DefaultSymbols()
	}

	period, skipped, err := h.resolvePeriod(interval, req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if skipped {
		return c.JSON(http.StatusAccepted, map[string]string{
			"status":   "skipped",
			"reason":   "no active period at given time",
			"interval": string(interval),
		})
	}

	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}

	ctx := c.Request().Context()
	result, err := h.alerts.Run(ctx, interval, period, symbols, dryRun)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, result)
}

func (h *Handler) resolvePeriod(interval domain.Interval, req runAlertRequest) (domain.Period, bool, error) {
	if req.PeriodStart != "" || req.PeriodEnd != "" {
		if req.PeriodStart == "" || req.PeriodEnd == "" {
			return domain.Period{}, false, echo.NewHTTPError(http.StatusBadRequest, "both period_start and period_end are required")
		}
		start, err := time.Parse(time.RFC3339, req.PeriodStart)
		if err != nil {
			return domain.Period{}, false, err
		}
		end, err := time.Parse(time.RFC3339, req.PeriodEnd)
		if err != nil {
			return domain.Period{}, false, err
		}
		period := domain.Period{Interval: interval, Start: start, End: end}
		if err := period.Validate(); err != nil {
			return domain.Period{}, false, err
		}
		return period, false, nil
	}

	now := time.Now()
	if req.At != "" {
		parsed, err := time.Parse(time.RFC3339, req.At)
		if err != nil {
			return domain.Period{}, false, err
		}
		now = parsed
	}
	period, ok := h.periods.GetCurrentPeriod(now, interval)
	if !ok {
		return domain.Period{}, true, nil
	}
	return period, false, nil
}
