package chat

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf16"

	"crypto-price-alert/internal/domain"
)

type FundingReader interface {
	FundingRates(context.Context, int) (domain.FundingRateResult, error)
}

const fundingUsage = "Usage: /funding [limit], limit 5–20 (default 5)."
const fundingTemporary = "Funding data temporarily unavailable. Please try again later."

func (s *Service) handleFunding(ctx context.Context, command Command) (string, error) {
	if command.Target.Provider != domain.ChatProviderTelegram {
		return "Funding lookup is available only in Telegram.", nil
	}
	limit := domain.DefaultFundingLimit
	if len(command.Arguments) > 1 {
		return fundingUsage, nil
	}
	if len(command.Arguments) == 1 {
		var err error
		limit, err = domain.ParseFundingLimit(command.Arguments[0])
		if err != nil {
			return fundingUsage, nil
		}
	}
	if s.funding == nil {
		return "Funding lookup is disabled or not configured.", nil
	}
	result, err := s.funding.FundingRates(ctx, limit)
	if err != nil {
		return fundingTemporary, nil
	}
	message, err := formatFunding(result)
	if err != nil {
		return fundingTemporary, nil
	}
	if s.publicIP != nil {
		ip := "unavailable"
		if address, err := s.publicIP.PublicIP(ctx); err == nil && address.IsValid() {
			ip = address.String()
		}
		message += "\nPublic IP: " + ip
		if len(utf16.Encode([]rune(message))) > 4096 {
			return fundingTemporary, nil
		}
	}
	return message, nil
}

func formatFunding(result domain.FundingRateResult) (string, error) {
	if len(result.Data) > domain.MaxFundingLimit {
		return "", fmt.Errorf("too many funding rows")
	}
	var b strings.Builder
	if len(result.Data) == 0 {
		b.WriteString("No eligible positive-rate pairs available.\n")
	}
	for i, row := range result.Data {
		if (domain.FundingContract{Symbol: row.Symbol, BaseAsset: row.BaseAsset, QuoteAsset: row.QuoteAsset}).Validate() != nil || row.IntervalHours <= 0 || row.IntervalHours > 24 {
			return "", fmt.Errorf("invalid funding row")
		}
		percent, err := domain.FundingPercent(row.Rate)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%d. %s | %s/%dh | next %s UTC\n", i+1, row.Symbol, percent, row.IntervalHours, row.NextFundingTime.UTC().Format("2006-01-02 15:04"))
	}
	if result.Shortfall {
		fmt.Fprintf(&b, "Only %d/%d positive pairs available.\n", result.ReturnedCount, result.RequestedLimit)
	}
	if len(utf16.Encode([]rune(b.String()))) > 4096 {
		return "", fmt.Errorf("funding message exceeds Telegram limit")
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
