package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/service"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type FundingReader interface {
	FundingRates(context.Context, int) (domain.FundingRateResult, error)
}

// RegisterFundingRoutes leaves existing handlers and middleware unchanged.
// A process-wide route budget avoids trusting forwarded IP headers and bounds
// public request load even when callers use many source addresses.
func RegisterFundingRoutes(e *echo.Echo, reader FundingReader) {
	limiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store:               middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{Rate: 2, Burst: 10}),
		IdentifierExtractor: func(echo.Context) (string, error) { return "funding", nil },
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			c.Response().Header().Set("Retry-After", "1")
			return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "too many funding requests", "code": "funding_rate_limited"})
		},
	})
	e.GET("/api/v1/funding-rates", fundingHandler(reader), limiter)
}

func fundingHandler(reader FundingReader) echo.HandlerFunc {
	return func(c echo.Context) error {
		query, err := url.ParseQuery(c.Request().URL.RawQuery)
		if err != nil {
			return fundingBadLimit(c)
		}
		limit := domain.DefaultFundingLimit
		if values, present := query["limit"]; present {
			if len(values) != 1 {
				return fundingBadLimit(c)
			}
			limit, err = domain.ParseFundingLimit(values[0])
		}
		if err != nil {
			return fundingBadLimit(c)
		}
		if reader == nil {
			return fundingHTTPError(c, service.ErrFundingUnavailable)
		}
		result, err := reader.FundingRates(c.Request().Context(), limit)
		if err != nil {
			return fundingHTTPError(c, err)
		}
		if result.Data == nil {
			result.Data = []domain.FundingRate{}
		}
		return c.JSON(http.StatusOK, result)
	}
}

func fundingBadLimit(c echo.Context) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be a single integer from 5 to 20", "code": "invalid_funding_limit"})
}

func fundingHTTPError(c echo.Context, err error) error {
	status, code, message := http.StatusBadGateway, "funding_upstream_error", "funding data temporarily unavailable"
	var upstream *market.FundingUpstreamError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusGatewayTimeout, "funding_timeout"

	case errors.Is(err, service.ErrFundingUnavailable):
		status, code = http.StatusServiceUnavailable, "funding_unavailable"

	case errors.As(err, &upstream):
		if upstream.Status == 418 || upstream.Status == 429 {
			status, code = http.StatusServiceUnavailable, "funding_upstream_throttled"
			if upstream.RetryAfter > 0 {
				c.Response().Header().Set("Retry-After", strconv.FormatInt(int64(math.Ceil(upstream.RetryAfter.Seconds())), 10))
			}
		}
	}

	return c.JSON(status, map[string]string{"error": message, "code": code})
}
