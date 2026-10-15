package domain

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultFundingLimit = 5
const MaxFundingLimit = 20

var fundingDecimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// Binance also lists Unicode assets (for example 币安人生USDT).
var fundingSymbol = regexp.MustCompile(`^[\p{L}\p{N}_]{1,32}$`)

// ParseFundingDecimal preserves exact numeric ordering without float rounding.
func ParseFundingDecimal(value string) (*big.Rat, error) {
	if len(value) > 64 || !fundingDecimal.MatchString(value) {
		return nil, fmt.Errorf("invalid funding decimal")
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid funding decimal")
	}
	return n, nil
}

func ParseFundingLimit(value string) (int, error) {
	if len(value) == 0 || len(value) > 2 {
		return 0, fmt.Errorf("limit must be an integer from 5 to 20")
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("limit must be an integer from 5 to 20")
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < DefaultFundingLimit || n > MaxFundingLimit {
		return 0, fmt.Errorf("limit must be an integer from 5 to 20")
	}
	return n, nil
}

// FundingPercent is display-only; the API retains the original decimal string.
func FundingPercent(value string) (string, error) {
	n, err := ParseFundingDecimal(value)
	if err != nil {
		return "", err
	}
	n.Mul(n, big.NewRat(100, 1))
	s := n.FloatString(8)
	if (n.Sign() != 0 && (s == "0.00000000" || s == "-0.00000000")) || len(s) > 20 {
		s = new(big.Float).SetPrec(256).SetRat(n).Text('g', 8)
	} else {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if n.Sign() > 0 {
		s = "+" + s
	}
	return s + "%", nil
}

type FundingContract struct {
	Symbol     string
	BaseAsset  string
	QuoteAsset string
}

func (c FundingContract) Validate() error {
	if !fundingSymbol.MatchString(c.Symbol) || !fundingSymbol.MatchString(c.BaseAsset) || (c.QuoteAsset != "USDT" && c.QuoteAsset != "USDC") {
		return fmt.Errorf("invalid funding contract")
	}
	return nil
}

type FundingObservation struct {
	Symbol          string
	Rate            string
	NextFundingTime time.Time
	ObservedAt      time.Time
}

// Validate enforces a 2-minute maximum observation age and 5-second clock skew.
func (o FundingObservation) Validate(now time.Time) error {
	if !fundingSymbol.MatchString(o.Symbol) {
		return fmt.Errorf("invalid funding symbol")
	}
	if _, err := ParseFundingDecimal(o.Rate); err != nil {
		return err
	}
	if o.ObservedAt.IsZero() || o.ObservedAt.Before(now.Add(-2*time.Minute)) || o.ObservedAt.After(now.Add(5*time.Second)) || !o.NextFundingTime.After(now) || o.NextFundingTime.After(now.Add(24*time.Hour)) {
		return fmt.Errorf("invalid or stale funding timestamp")
	}
	return nil
}

type FundingRate struct {
	Symbol          string    `json:"symbol"`
	BaseAsset       string    `json:"base_asset"`
	QuoteAsset      string    `json:"quote_asset"`
	Rate            string    `json:"funding_rate"`
	IntervalHours   int       `json:"funding_interval_hours"`
	NextFundingTime time.Time `json:"next_funding_time"`
	ObservedAt      time.Time `json:"observed_at"`
}

type FundingRateResult struct {
	Source                string        `json:"source"`
	RateSourceField       string        `json:"rate_source_field"`
	RequestedLimit        int           `json:"requested_limit"`
	ReturnedCount         int           `json:"returned_count"`
	EligiblePositiveCount int           `json:"eligible_positive_count"`
	Shortfall             bool          `json:"shortfall"`
	GeneratedAt           time.Time     `json:"generated_at"`
	ObservedAt            *time.Time    `json:"observed_at"`
	ExcludedInvalidCount  int           `json:"excluded_invalid_count"`
	Data                  []FundingRate `json:"data"`
}
