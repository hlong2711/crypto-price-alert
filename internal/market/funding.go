package market

import (
	"context"
	"fmt"
	"time"

	"crypto-price-alert/internal/domain"
)

// FundingDataProvider is independent of the spot kline provider.
type FundingDataProvider interface {
	FundingContracts(context.Context) ([]domain.FundingContract, error)
	FundingIntervals(context.Context) (map[string]int, error)
	FundingObservations(context.Context) ([]domain.FundingObservation, error)
}

// FundingUpstreamError deliberately excludes upstream payloads and URLs.
type FundingUpstreamError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *FundingUpstreamError) Error() string {
	return fmt.Sprintf("Binance futures unavailable (status %d)", e.Status)
}

func invalidFundingPayload() error { return &FundingUpstreamError{Status: 502} }
